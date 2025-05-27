package tunnel

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"ssh-tunnel-system/pkg/config"

	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
)

// Server represents the tunnel server
type Server struct {
	config     *config.ServerConfig
	sshServer  *ssh.Server
	listener   net.Listener
	clients    map[string]*ClientConnection
	portPool   *PortPool
	hostKey    ssh.Signer
	authorizedKeys map[string]ssh.PublicKey
	mu         sync.RWMutex
	done       chan struct{}
}

// ClientConnection represents a connected client
type ClientConnection struct {
	ID           string
	Conn         ssh.Conn
	Channels     <-chan ssh.NewChannel
	Requests     <-chan *ssh.Request
	AssignedPort int
	Tunnels      map[string]*Tunnel
	LastSeen     int64
	mu           sync.RWMutex
}

// Tunnel represents an active tunnel
type Tunnel struct {
	Name       string
	LocalHost  string
	LocalPort  int
	RemotePort int
	Protocol   string
	Listener   net.Listener
	Type       string // "forward" or "reverse"
	Active     bool
	CreatedAt  time.Time
}

// PortPool manages available ports for tunneling
type PortPool struct {
	start     int
	end       int
	used      map[int]bool
	available []int
	mu        sync.Mutex
}

// TCPIPForwardPayload represents the payload for tcpip-forward requests
type TCPIPForwardPayload struct {
	BindIP   string
	BindPort uint32
}

// NewServer creates a new tunnel server
func NewServer(config *config.ServerConfig) (*Server, error) {
	// Create port pool
	portPool := &PortPool{
		start: config.Server.PortRange.Start,
		end:   config.Server.PortRange.End,
		used:  make(map[int]bool),
	}
	
	// Initialize available ports
	for i := portPool.start; i <= portPool.end; i++ {
		portPool.available = append(portPool.available, i)
	}

	server := &Server{
		config:         config,
		clients:        make(map[string]*ClientConnection),
		portPool:       portPool,
		authorizedKeys: make(map[string]ssh.PublicKey),
		done:           make(chan struct{}),
	}

	// Load or generate host key
	hostKey, err := server.loadOrGenerateHostKey()
	if err != nil {
		return nil, fmt.Errorf("failed to load/generate host key: %v", err)
	}
	server.hostKey = hostKey

	// Load authorized keys
	if err := server.loadAuthorizedKeys(); err != nil {
		logrus.WithError(err).Warn("Failed to load authorized keys, authentication disabled")
	}

	return server, nil
}

// Start starts the tunnel server
func (s *Server) Start() error {
	// Configure SSH server
	sshConfig := &ssh.ServerConfig{
		PublicKeyCallback: s.publicKeyAuth,
		ServerVersion:     "SSH-2.0-TunnelServer_1.0",
	}
	sshConfig.AddHostKey(s.hostKey)

	// Start listening for SSH connections
	address := fmt.Sprintf("%s:%d", s.config.Server.SSHHost, s.config.Server.SSHPort)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %v", address, err)
	}

	s.listener = listener
	logrus.WithFields(logrus.Fields{
		"address":    address,
		"port_range": fmt.Sprintf("%d-%d", s.config.Server.PortRange.Start, s.config.Server.PortRange.End),
	}).Info("SSH tunnel server started")

	// Accept connections in goroutine
	go s.acceptConnections(sshConfig)

	return nil
}

// Stop stops the tunnel server
func (s *Server) Stop() {
	close(s.done)
	
	if s.listener != nil {
		s.listener.Close()
	}

	// Close all client connections and their tunnels
	s.mu.Lock()
	for _, client := range s.clients {
		client.mu.Lock()
		for _, tunnel := range client.Tunnels {
			if tunnel.Listener != nil {
				tunnel.Listener.Close()
			}
		}
		client.mu.Unlock()
		client.Conn.Close()
	}
	s.mu.Unlock()

	logrus.Info("SSH tunnel server stopped")
}

// acceptConnections accepts and handles SSH connections
func (s *Server) acceptConnections(sshConfig *ssh.ServerConfig) {
	for {
		select {
		case <-s.done:
			return
		default:
			conn, err := s.listener.Accept()
			if err != nil {
				select {
				case <-s.done:
					return
				default:
					logrus.WithError(err).Error("Failed to accept connection")
					continue
				}
			}

			go s.handleConnection(conn, sshConfig)
		}
	}
}

// handleConnection handles a single SSH connection
func (s *Server) handleConnection(conn net.Conn, sshConfig *ssh.ServerConfig) {
	defer conn.Close()

	// SSH handshake
	sshConn, chans, reqs, err := ssh.NewServerConn(conn, sshConfig)
	if err != nil {
		logrus.WithError(err).WithField("remote_addr", conn.RemoteAddr()).Error("SSH handshake failed")
		return
	}
	defer sshConn.Close()

	clientID := sshConn.User()
	logrus.WithFields(logrus.Fields{
		"client_id":   clientID,
		"remote_addr": conn.RemoteAddr(),
		"version":     string(sshConn.ClientVersion()),
	}).Info("Client connected")

	// Assign port for this client's SSH access
	port, err := s.portPool.AllocatePort()
	if err != nil {
		logrus.WithError(err).Error("Failed to allocate port for client")
		return
	}
	defer s.portPool.ReleasePort(port)

	// Create client connection record
	client := &ClientConnection{
		ID:           clientID,
		Conn:         sshConn,
		Channels:     chans,
		Requests:     reqs,
		AssignedPort: port,
		Tunnels:      make(map[string]*Tunnel),
		LastSeen:     time.Now().Unix(),
	}

	// Register client
	s.mu.Lock()
	s.clients[clientID] = client
	s.mu.Unlock()

	// Clean up on disconnect
	defer func() {
		s.mu.Lock()
		delete(s.clients, clientID)
		s.mu.Unlock()
		
		// Close all tunnels for this client
		client.mu.Lock()
		for _, tunnel := range client.Tunnels {
			if tunnel.Listener != nil {
				tunnel.Listener.Close()
			}
		}
		client.mu.Unlock()
		
		logrus.WithField("client_id", clientID).Info("Client disconnected")
	}()

	// Handle SSH channels and requests
	s.handleSSHChannels(client)
}

// handleSSHChannels handles SSH channels for a client
func (s *Server) handleSSHChannels(client *ClientConnection) {
	for {
		select {
		case newChannel := <-client.Channels:
			if newChannel == nil {
				return
			}
			go s.handleNewChannel(client, newChannel)

		case req := <-client.Requests:
			if req == nil {
				return
			}
			go s.handleGlobalRequest(client, req)
		}
	}
}

// handleNewChannel handles new SSH channel requests
func (s *Server) handleNewChannel(client *ClientConnection, newChannel ssh.NewChannel) {
	switch newChannel.ChannelType() {
	case "direct-tcpip":
		s.handleDirectTCPIP(client, newChannel)
	case "session":
		s.handleSessionChannel(client, newChannel)
	default:
		logrus.WithFields(logrus.Fields{
			"client_id":    client.ID,
			"channel_type": newChannel.ChannelType(),
		}).Debug("Rejecting unknown channel type")
		newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
	}
}

// handleDirectTCPIP handles direct TCP/IP forwarding (local forwarding)
func (s *Server) handleDirectTCPIP(client *ClientConnection, newChannel ssh.NewChannel) {
	// Parse the payload to get destination info
	var payload struct {
		DestHost string
		DestPort uint32
		OrigHost string
		OrigPort uint32
	}

	if err := ssh.Unmarshal(newChannel.ExtraData(), &payload); err != nil {
		logrus.WithError(err).Error("Failed to parse direct-tcpip payload")
		newChannel.Reject(ssh.ConnectionFailed, "invalid payload")
		return
	}

	logrus.WithFields(logrus.Fields{
		"client_id": client.ID,
		"dest":      fmt.Sprintf("%s:%d", payload.DestHost, payload.DestPort),
		"orig":      fmt.Sprintf("%s:%d", payload.OrigHost, payload.OrigPort),
	}).Debug("Handling direct-tcpip channel")

	// Connect to the destination
	destAddr := fmt.Sprintf("%s:%d", payload.DestHost, payload.DestPort)
	destConn, err := net.Dial("tcp", destAddr)
	if err != nil {
		logrus.WithError(err).WithField("dest", destAddr).Error("Failed to connect to destination")
		newChannel.Reject(ssh.ConnectionFailed, "connection failed")
		return
	}
	defer destConn.Close()

	// Accept the channel
	channel, requests, err := newChannel.Accept()
	if err != nil {
		logrus.WithError(err).Error("Failed to accept direct-tcpip channel")
		return
	}
	defer channel.Close()

	// Discard requests
	go ssh.DiscardRequests(requests)

	// Forward data bidirectionally
	go s.forwardConnection(channel, destConn)
}

// handleSessionChannel handles SSH session channels (for shell access)
func (s *Server) handleSessionChannel(client *ClientConnection, newChannel ssh.NewChannel) {
	// For now, reject session channels as we're only doing tunneling
	logrus.WithField("client_id", client.ID).Debug("Rejecting session channel - tunneling only")
	newChannel.Reject(ssh.Prohibited, "shell sessions not supported")
}

// handleGlobalRequest handles SSH global requests
func (s *Server) handleGlobalRequest(client *ClientConnection, req *ssh.Request) {
	client.LastSeen = time.Now().Unix()

	switch req.Type {
	case "tcpip-forward":
		s.handleTCPIPForward(client, req)
	case "cancel-tcpip-forward":
		s.handleCancelTCPIPForward(client, req)
	default:
		logrus.WithFields(logrus.Fields{
			"client_id": client.ID,
			"request":   req.Type,
		}).Debug("Unknown global request")
		if req.WantReply {
			req.Reply(false, nil)
		}
	}
}

// handleTCPIPForward handles reverse port forwarding requests
func (s *Server) handleTCPIPForward(client *ClientConnection, req *ssh.Request) {
	var payload TCPIPForwardPayload
	if err := ssh.Unmarshal(req.Payload, &payload); err != nil {
		logrus.WithError(err).Error("Failed to parse tcpip-forward payload")
		if req.WantReply {
			req.Reply(false, nil)
		}
		return
	}

	// Use assigned port if bind port is 0, otherwise use requested port
	var bindPort int
	if payload.BindPort == 0 {
		bindPort = client.AssignedPort
	} else {
		bindPort = int(payload.BindPort)
		// Check if port is available
		if !s.portPool.IsPortAvailable(bindPort) {
			logrus.WithFields(logrus.Fields{
				"client_id": client.ID,
				"port":      bindPort,
			}).Error("Requested port not available")
			if req.WantReply {
				req.Reply(false, nil)
			}
			return
		}
		// Reserve the port
		if err := s.portPool.ReservePort(bindPort); err != nil {
			logrus.WithError(err).Error("Failed to reserve port")
			if req.WantReply {
				req.Reply(false, nil)
			}
			return
		}
	}

	// Start listening on the bind address
	bindAddr := fmt.Sprintf("%s:%d", payload.BindIP, bindPort)
	if payload.BindIP == "" || payload.BindIP == "0.0.0.0" {
		bindAddr = fmt.Sprintf(":%d", bindPort)
	}

	listener, err := net.Listen("tcp", bindAddr)
	if err != nil {
		logrus.WithError(err).WithField("bind_addr", bindAddr).Error("Failed to start reverse tunnel listener")
		if req.WantReply {
			req.Reply(false, nil)
		}
		return
	}

	// Create tunnel record
	tunnelName := fmt.Sprintf("reverse-%s-%d", payload.BindIP, bindPort)
	tunnel := &Tunnel{
		Name:       tunnelName,
		LocalHost:  payload.BindIP,
		LocalPort:  bindPort,
		RemotePort: int(payload.BindPort),
		Protocol:   "tcp",
		Listener:   listener,
		Type:       "reverse",
		Active:     true,
		CreatedAt:  time.Now(),
	}

	// Register tunnel
	client.mu.Lock()
	client.Tunnels[tunnelName] = tunnel
	client.mu.Unlock()

	logrus.WithFields(logrus.Fields{
		"client_id":  client.ID,
		"bind_addr":  bindAddr,
		"tunnel":     tunnelName,
	}).Info("Reverse tunnel established")

	// Reply with the actual port used
	replyPayload := make([]byte, 4)
	binary.BigEndian.PutUint32(replyPayload, uint32(bindPort))
	if req.WantReply {
		req.Reply(true, replyPayload)
	}

	// Handle incoming connections in a goroutine
	go s.handleReverseConnections(client, tunnel)
}

// handleReverseConnections handles incoming connections for reverse tunnels
func (s *Server) handleReverseConnections(client *ClientConnection, tunnel *Tunnel) {
	defer tunnel.Listener.Close()
	defer func() {
		client.mu.Lock()
		delete(client.Tunnels, tunnel.Name)
		client.mu.Unlock()
		s.portPool.ReleasePort(tunnel.LocalPort)
		logrus.WithFields(logrus.Fields{
			"client_id": client.ID,
			"tunnel":    tunnel.Name,
		}).Info("Reverse tunnel closed")
	}()

	for {
		conn, err := tunnel.Listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
				logrus.WithError(err).WithField("tunnel", tunnel.Name).Debug("Reverse tunnel listener closed")
				return
			}
		}

		go s.handleReverseConnection(client, tunnel, conn)
	}
}

// handleReverseConnection handles a single reverse connection
func (s *Server) handleReverseConnection(client *ClientConnection, tunnel *Tunnel, conn net.Conn) {
	defer conn.Close()

	// Create forwarded-tcpip channel to client
	payload := struct {
		ConnectedAddress string
		ConnectedPort    uint32
		OriginatorAddress string
		OriginatorPort   uint32
	}{
		ConnectedAddress:  tunnel.LocalHost,
		ConnectedPort:     uint32(tunnel.LocalPort),
		OriginatorAddress: conn.RemoteAddr().(*net.TCPAddr).IP.String(),
		OriginatorPort:    uint32(conn.RemoteAddr().(*net.TCPAddr).Port),
	}

	channel, reqs, err := client.Conn.OpenChannel("forwarded-tcpip", ssh.Marshal(&payload))
	if err != nil {
		logrus.WithError(err).WithField("client_id", client.ID).Error("Failed to open forwarded-tcpip channel")
		return
	}
	defer channel.Close()

	// Discard requests
	go ssh.DiscardRequests(reqs)

	// Forward data bidirectionally
	s.forwardConnection(conn, channel)
}

// handleCancelTCPIPForward handles cancel reverse port forwarding
func (s *Server) handleCancelTCPIPForward(client *ClientConnection, req *ssh.Request) {
	var payload TCPIPForwardPayload
	if err := ssh.Unmarshal(req.Payload, &payload); err != nil {
		logrus.WithError(err).Error("Failed to parse cancel-tcpip-forward payload")
		if req.WantReply {
			req.Reply(false, nil)
		}
		return
	}

	tunnelName := fmt.Sprintf("reverse-%s-%d", payload.BindIP, payload.BindPort)
	
	client.mu.Lock()
	tunnel, exists := client.Tunnels[tunnelName]
	if exists {
		delete(client.Tunnels, tunnelName)
	}
	client.mu.Unlock()

	if exists && tunnel.Listener != nil {
		tunnel.Listener.Close()
		s.portPool.ReleasePort(tunnel.LocalPort)
		logrus.WithFields(logrus.Fields{
			"client_id": client.ID,
			"tunnel":    tunnelName,
		}).Info("Reverse tunnel cancelled")
	}

	if req.WantReply {
		req.Reply(true, nil)
	}
}

// forwardConnection forwards data between two connections
func (s *Server) forwardConnection(conn1 io.ReadWriteCloser, conn2 io.ReadWriteCloser) {
	var wg sync.WaitGroup
	wg.Add(2)

	// Forward conn1 -> conn2
	go func() {
		defer wg.Done()
		io.Copy(conn2, conn1)
		if c, ok := conn2.(interface{ CloseWrite() error }); ok {
			c.CloseWrite()
		}
	}()

	// Forward conn2 -> conn1
	go func() {
		defer wg.Done()
		io.Copy(conn1, conn2)
		if c, ok := conn1.(interface{ CloseWrite() error }); ok {
			c.CloseWrite()
		}
	}()

	wg.Wait()
}

// loadOrGenerateHostKey loads existing host key or generates a new one
func (s *Server) loadOrGenerateHostKey() (ssh.Signer, error) {
	keyPath := s.config.Server.HostKeyPath
	if keyPath == "" {
		keyPath = "keys/ssh_host_ed25519_key"
	}

	// Try to load existing key
	if _, err := os.Stat(keyPath); err == nil {
		keyData, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read host key: %v", err)
		}

		key, err := ssh.ParsePrivateKey(keyData)
		if err != nil {
			return nil, fmt.Errorf("failed to parse host key: %v", err)
		}

		logrus.WithField("key_path", keyPath).Info("Loaded existing host key")
		return key, nil
	}

	// Generate new key
	logrus.WithField("key_path", keyPath).Info("Generating new host key")
	key, err := generateEd25519Key()
	if err != nil {
		return nil, fmt.Errorf("failed to generate host key: %v", err)
	}

	// Save key to file
	keyDir := filepath.Dir(keyPath)
	if err := os.MkdirAll(keyDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create key directory: %v", err)
	}

	keyBytes := ssh.MarshalPrivateKey(key, "")
	if err := os.WriteFile(keyPath, keyBytes, 0600); err != nil {
		return nil, fmt.Errorf("failed to save host key: %v", err)
	}

	logrus.WithField("key_path", keyPath).Info("Generated and saved new host key")
	return key, nil
}

// loadAuthorizedKeys loads authorized keys from file
func (s *Server) loadAuthorizedKeys() error {
	keyPath := s.config.Server.AuthorizedKeysPath
	if keyPath == "" {
		keyPath = "keys/authorized_keys"
	}

	data, err := os.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("failed to read authorized keys: %v", err)
	}

	lines := strings.Split(string(data), "\n")
	count := 0
	
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, comment, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
		if err != nil {
			logrus.WithError(err).WithField("line", i+1).Warn("Failed to parse authorized key")
			continue
		}

		keyID := comment
		if keyID == "" {
			keyID = fmt.Sprintf("key-%d", i)
		}

		s.authorizedKeys[keyID] = key
		count++
	}

	logrus.WithFields(logrus.Fields{
		"key_path": keyPath,
		"count":    count,
	}).Info("Loaded authorized keys")

	return nil
}

// publicKeyAuth handles SSH public key authentication
func (s *Server) publicKeyAuth(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
	logrus.WithFields(logrus.Fields{
		"user":       conn.User(),
		"key_type":   key.Type(),
		"remote_addr": conn.RemoteAddr(),
	}).Debug("Public key authentication attempt")

	// If no authorized keys loaded, accept all (development mode)
	if len(s.authorizedKeys) == 0 {
		logrus.WithField("user", conn.User()).Warn("No authorized keys configured - accepting all keys")
		return &ssh.Permissions{
			Extensions: map[string]string{
				"client-id": conn.User(),
			},
		}, nil
	}

	// Check against authorized keys
	keyBytes := key.Marshal()
	for keyID, authKey := range s.authorizedKeys {
		if string(authKey.Marshal()) == string(keyBytes) {
			logrus.WithFields(logrus.Fields{
				"user":   conn.User(),
				"key_id": keyID,
			}).Info("Public key authentication successful")
			
			return &ssh.Permissions{
				Extensions: map[string]string{
					"client-id": conn.User(),
					"key-id":    keyID,
				},
			}, nil
		}
	}

	logrus.WithField("user", conn.User()).Warn("Public key authentication failed")
	return nil, fmt.Errorf("public key not authorized")
}

// AllocatePort allocates an available port from the pool
func (p *PortPool) AllocatePort() (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.available) == 0 {
		return 0, fmt.Errorf("no available ports")
	}

	port := p.available[0]
	p.available = p.available[1:]
	p.used[port] = true

	return port, nil
}

// ReleasePort releases a port back to the pool
func (p *PortPool) ReleasePort(port int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.used[port] {
		delete(p.used, port)
		p.available = append(p.available, port)
	}
}

// IsPortAvailable checks if a port is available
func (p *PortPool) IsPortAvailable(port int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if port < p.start || port > p.end {
		return false
	}

	return !p.used[port]
}

// ReservePort reserves a specific port
func (p *PortPool) ReservePort(port int) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if port < p.start || port > p.end {
		return fmt.Errorf("port %d out of range [%d-%d]", port, p.start, p.end)
	}

	if p.used[port] {
		return fmt.Errorf("port %d already in use", port)
	}

	// Remove from available list
	for i, availPort := range p.available {
		if availPort == port {
			p.available = append(p.available[:i], p.available[i+1:]...)
			break
		}
	}

	p.used[port] = true
	return nil
}

// GetClientStats returns statistics about connected clients
func (s *Server) GetClientStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := map[string]interface{}{
		"total_clients": len(s.clients),
		"clients":       make([]map[string]interface{}, 0),
	}

	for _, client := range s.clients {
		client.mu.RLock()
		clientStats := map[string]interface{}{
			"id":            client.ID,
			"assigned_port": client.AssignedPort,
			"tunnels":       len(client.Tunnels),
			"last_seen":     client.LastSeen,
		}
		client.mu.RUnlock()
		
		stats["clients"] = append(stats["clients"].([]map[string]interface{}), clientStats)
	}

	return stats
}
