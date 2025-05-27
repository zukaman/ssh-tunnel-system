package tunnel

import (
	"fmt"
	"net"
	"sync"

	"ssh-tunnel-system/pkg/config"

	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
)

// Server represents the tunnel server
type Server struct {
	config   *config.ServerConfig
	sshServer *ssh.Server
	listener  net.Listener
	clients   map[string]*ClientConnection
	portPool  *PortPool
	mu        sync.RWMutex
	done      chan struct{}
}

// ClientConnection represents a connected client
type ClientConnection struct {
	ID         string
	Conn       ssh.Conn
	Channels   <-chan ssh.NewChannel
	Requests   <-chan *ssh.Request
	AssignedPort int
	Tunnels    map[string]*Tunnel
	LastSeen   int64
}

// Tunnel represents an active tunnel
type Tunnel struct {
	Name       string
	LocalHost  string
	LocalPort  int
	RemotePort int
	Protocol   string
	Listener   net.Listener
}

// PortPool manages available ports for tunneling
type PortPool struct {
	start     int
	end       int
	used      map[int]bool
	available []int
	mu        sync.Mutex
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
		config:   config,
		clients:  make(map[string]*ClientConnection),
		portPool: portPool,
		done:     make(chan struct{}),
	}

	return server, nil
}

// Start starts the tunnel server
func (s *Server) Start() error {
	// TODO: Load or generate SSH host key
	hostKey, err := s.loadHostKey()
	if err != nil {
		return fmt.Errorf("failed to load host key: %v", err)
	}

	// Configure SSH server
	sshConfig := &ssh.ServerConfig{
		PublicKeyCallback: s.publicKeyAuth,
	}
	sshConfig.AddHostKey(hostKey)

	// Start listening for SSH connections
	address := fmt.Sprintf("%s:%d", s.config.Server.SSHHost, s.config.Server.SSHPort)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %v", address, err)
	}

	s.listener = listener
	logrus.WithField("address", address).Info("SSH tunnel server listening")

	// Accept connections in goroutine
	go s.acceptConnections(sshConfig)

	// TODO: Start gRPC management API
	// go s.startGRPCServer()

	return nil
}

// Stop stops the tunnel server
func (s *Server) Stop() {
	close(s.done)
	
	if s.listener != nil {
		s.listener.Close()
	}

	// Close all client connections
	s.mu.Lock()
	for _, client := range s.clients {
		client.Conn.Close()
	}
	s.mu.Unlock()
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
				logrus.WithError(err).Error("Failed to accept connection")
				continue
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
		logrus.WithError(err).Error("Failed to handshake SSH connection")
		return
	}
	defer sshConn.Close()

	clientID := sshConn.User()
	logrus.WithField("client_id", clientID).Info("Client connected")

	// Assign port for this client
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
	default:
		newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
	}
}

// handleDirectTCPIP handles direct TCP/IP forwarding
func (s *Server) handleDirectTCPIP(client *ClientConnection, newChannel ssh.NewChannel) {
	// TODO: Implement TCP forwarding logic
	logrus.WithFields(logrus.Fields{
		"client_id": client.ID,
		"channel":   "direct-tcpip",
	}).Debug("Handling direct-tcpip channel")

	channel, requests, err := newChannel.Accept()
	if err != nil {
		logrus.WithError(err).Error("Failed to accept channel")
		return
	}
	defer channel.Close()

	// Discard requests for now
	go ssh.DiscardRequests(requests)

	// TODO: Forward data between channel and target
}

// handleGlobalRequest handles SSH global requests
func (s *Server) handleGlobalRequest(client *ClientConnection, req *ssh.Request) {
	switch req.Type {
	case "tcpip-forward":
		s.handleTCPIPForward(client, req)
	case "cancel-tcpip-forward":
		s.handleCancelTCPIPForward(client, req)
	default:
		if req.WantReply {
			req.Reply(false, nil)
		}
	}
}

// handleTCPIPForward handles reverse port forwarding requests
func (s *Server) handleTCPIPForward(client *ClientConnection, req *ssh.Request) {
	// TODO: Implement reverse port forwarding
	logrus.WithFields(logrus.Fields{
		"client_id": client.ID,
		"request":   "tcpip-forward",
	}).Debug("Handling tcpip-forward request")

	if req.WantReply {
		req.Reply(true, nil)
	}
}

// handleCancelTCPIPForward handles cancel reverse port forwarding
func (s *Server) handleCancelTCPIPForward(client *ClientConnection, req *ssh.Request) {
	// TODO: Implement cancel reverse port forwarding
	logrus.WithFields(logrus.Fields{
		"client_id": client.ID,
		"request":   "cancel-tcpip-forward",
	}).Debug("Handling cancel-tcpip-forward request")

	if req.WantReply {
		req.Reply(true, nil)
	}
}

// loadHostKey loads or generates SSH host key
func (s *Server) loadHostKey() (ssh.Signer, error) {
	// TODO: Implement host key loading/generation
	// For now, generate a temporary key
	return generateEd25519Key()
}

// publicKeyAuth handles SSH public key authentication
func (s *Server) publicKeyAuth(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
	// TODO: Implement proper public key authentication
	// For now, accept all keys for development
	logrus.WithFields(logrus.Fields{
		"user":     conn.User(),
		"key_type": key.Type(),
	}).Info("Client authentication attempt")

	return &ssh.Permissions{
		Extensions: map[string]string{
			"client-id": conn.User(),
		},
	}, nil
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
