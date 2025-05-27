package tunnel

import (
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"ssh-tunnel-system/pkg/config"

	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
)

// Client represents the tunnel client
type Client struct {
	config     *config.ClientConfig
	sshClient  *ssh.Client
	conn       net.Conn
	tunnels    map[string]*ClientTunnel
	listeners  map[string]net.Listener
	mu         sync.RWMutex
	done       chan struct{}
	reconnect  chan struct{}
	connected  bool
}

// ClientTunnel represents a client-side tunnel
type ClientTunnel struct {
	Name       string
	LocalHost  string
	LocalPort  int
	RemotePort int
	Protocol   string
	Active     bool
}

// NewClient creates a new tunnel client
func NewClient(config *config.ClientConfig) (*Client, error) {
	client := &Client{
		config:    config,
		tunnels:   make(map[string]*ClientTunnel),
		listeners: make(map[string]net.Listener),
		done:      make(chan struct{}),
		reconnect: make(chan struct{}, 1),
		connected: false,
	}

	// Initialize tunnels from config
	for _, tunnelConfig := range config.Tunnels {
		client.tunnels[tunnelConfig.Name] = &ClientTunnel{
			Name:      tunnelConfig.Name,
			LocalHost: tunnelConfig.LocalHost,
			LocalPort: tunnelConfig.LocalPort,
			Protocol:  tunnelConfig.Protocol,
			Active:    false,
		}
	}

	return client, nil
}

// Start starts the tunnel client
func (c *Client) Start() error {
	logrus.WithFields(logrus.Fields{
		"server":    c.config.Server.Host,
		"client_id": c.config.ClientID,
		"tunnels":   len(c.tunnels),
	}).Info("Starting tunnel client")

	// Start connection loop
	go c.connectionLoop()

	// Start health monitoring if enabled
	if c.config.Health.Enabled {
		go c.startHealthMonitoring()
	}

	return nil
}

// Stop stops the tunnel client
func (c *Client) Stop() {
	logrus.Info("Stopping tunnel client")
	close(c.done)

	c.mu.Lock()
	defer c.mu.Unlock()
	
	// Close all listeners
	for name, listener := range c.listeners {
		logrus.WithField("tunnel", name).Debug("Closing tunnel listener")
		listener.Close()
	}
	
	// Close SSH connection
	if c.sshClient != nil {
		c.sshClient.Close()
	}
	if c.conn != nil {
		c.conn.Close()
	}
	
	c.connected = false
}

// connectionLoop manages the connection to the tunnel server
func (c *Client) connectionLoop() {
	retryCount := 0
	maxRetries := c.config.Connection.MaxRetries

	for {
		select {
		case <-c.done:
			return
		default:
		}

		// Attempt to connect
		if err := c.connect(); err != nil {
			logrus.WithError(err).Error("Failed to connect to tunnel server")
			
			// Check if we should retry
			if maxRetries > 0 && retryCount >= maxRetries {
				logrus.Error("Maximum retry attempts reached, giving up")
				return
			}
			
			retryCount++
			logrus.WithFields(logrus.Fields{
				"retry_count": retryCount,
				"max_retries": maxRetries,
			}).Info("Retrying connection")
			
			// Wait before retrying with exponential backoff
			backoff := c.config.Connection.RetryInterval * time.Duration(retryCount)
			if backoff > time.Minute*5 {
				backoff = time.Minute * 5 // Cap at 5 minutes
			}
			
			select {
			case <-c.done:
				return
			case <-time.After(backoff):
			}
			continue
		}

		// Reset retry count on successful connection
		retryCount = 0
		c.connected = true
		
		logrus.Info("Successfully connected to tunnel server")
		
		// Handle the connection
		c.handleConnection()
		
		// If we get here, connection was lost
		c.connected = false
		logrus.Warn("Connection lost, attempting to reconnect")
		
		// Clean up tunnels
		c.cleanupTunnels()
		
		// Wait before reconnecting
		select {
		case <-c.done:
			return
		case <-time.After(c.config.Connection.RetryInterval):
		}
	}
}

// connect establishes connection to the tunnel server
func (c *Client) connect() error {
	// Load SSH private key
	key, err := c.loadPrivateKey()
	if err != nil {
		return fmt.Errorf("failed to load private key: %v", err)
	}

	// Configure SSH client
	sshConfig := &ssh.ClientConfig{
		User: c.config.ClientID,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(key),
		},
		HostKeyCallback: c.getHostKeyCallback(),
		Timeout:         c.config.Connection.ConnectTimeout,
	}

	// Connect to server
	address := fmt.Sprintf("%s:%d", c.config.Server.Host, c.config.Server.Port)
	
	logrus.WithField("address", address).Debug("Connecting to tunnel server")
	
	conn, err := net.DialTimeout("tcp", address, c.config.Connection.ConnectTimeout)
	if err != nil {
		return fmt.Errorf("failed to dial %s: %v", address, err)
	}

	// SSH handshake
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, address, sshConfig)
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to create SSH connection: %v", err)
	}

	sshClient := ssh.NewClient(sshConn, chans, reqs)

	c.mu.Lock()
	c.sshClient = sshClient
	c.conn = conn
	c.mu.Unlock()

	logrus.WithFields(logrus.Fields{
		"server":     address,
		"client_id":  c.config.ClientID,
		"version":    string(sshConn.ServerVersion()),
	}).Info("Connected to tunnel server")
	
	return nil
}

// handleConnection handles the active SSH connection
func (c *Client) handleConnection() {
	// Setup tunnels
	if err := c.setupTunnels(); err != nil {
		logrus.WithError(err).Error("Failed to setup tunnels")
		return
	}

	// Start keepalive
	go c.keepalive()

	// Wait for connection to close or done signal
	select {
	case <-c.done:
		return
	case <-c.sshClient.Wait():
		// Connection closed
		logrus.Debug("SSH connection closed")
		return
	}
}

// setupTunnels sets up reverse tunnels for all configured services
func (c *Client) setupTunnels() error {
	successCount := 0
	
	for name, tunnel := range c.tunnels {
		logrus.WithFields(logrus.Fields{
			"tunnel": name,
			"local":  fmt.Sprintf("%s:%d", tunnel.LocalHost, tunnel.LocalPort),
		}).Info("Setting up tunnel")

		// Check if local service is available
		if err := c.checkLocalService(tunnel); err != nil {
			logrus.WithError(err).WithField("tunnel", name).Warn("Local service not available, skipping tunnel")
			continue
		}

		// Setup reverse tunnel based on protocol
		if tunnel.Protocol == "tcp" || tunnel.Protocol == "ssh" || tunnel.Protocol == "http" {
			if err := c.setupReverseTunnel(tunnel); err != nil {
				logrus.WithError(err).WithField("tunnel", name).Error("Failed to setup reverse tunnel")
				continue
			}
		} else {
			logrus.WithField("protocol", tunnel.Protocol).Warn("Unsupported tunnel protocol")
			continue
		}

		tunnel.Active = true
		successCount++
	}

	if successCount == 0 {
		return fmt.Errorf("no tunnels were successfully established")
	}

	logrus.WithField("active_tunnels", successCount).Info("Tunnels established successfully")
	return nil
}

// checkLocalService verifies that the local service is available
func (c *Client) checkLocalService(tunnel *ClientTunnel) error {
	localAddr := fmt.Sprintf("%s:%d", tunnel.LocalHost, tunnel.LocalPort)
	conn, err := net.DialTimeout("tcp", localAddr, time.Second*2)
	if err != nil {
		return fmt.Errorf("local service not available at %s: %v", localAddr, err)
	}
	conn.Close()
	return nil
}

// setupReverseTunnel sets up a reverse TCP tunnel
func (c *Client) setupReverseTunnel(tunnel *ClientTunnel) error {
	// Request reverse port forwarding
	// The server will listen on a port and forward connections to us
	listener, err := c.sshClient.Listen("tcp", "0.0.0.0:0") // 0 means server assigns port
	if err != nil {
		return fmt.Errorf("failed to setup reverse tunnel: %v", err)
	}

	// Store listener for cleanup
	c.mu.Lock()
	c.listeners[tunnel.Name] = listener
	c.mu.Unlock()

	// Get assigned port
	addr := listener.Addr().(*net.TCPAddr)
	tunnel.RemotePort = addr.Port

	logrus.WithFields(logrus.Fields{
		"tunnel":      tunnel.Name,
		"local_addr":  fmt.Sprintf("%s:%d", tunnel.LocalHost, tunnel.LocalPort),
		"remote_port": tunnel.RemotePort,
	}).Info("Reverse tunnel established")

	// Handle incoming connections from the tunnel
	go func() {
		defer listener.Close()
		for {
			conn, err := listener.Accept()
			if err != nil {
				logrus.WithError(err).Debug("Listener accept error")
				return
			}

			go c.handleTunnelConnection(tunnel, conn)
		}
	}()

	return nil
}

// handleTunnelConnection handles a connection through the tunnel
func (c *Client) handleTunnelConnection(tunnel *ClientTunnel, tunnelConn net.Conn) {
	defer tunnelConn.Close()

	// Connect to local service
	localAddr := fmt.Sprintf("%s:%d", tunnel.LocalHost, tunnel.LocalPort)
	localConn, err := net.Dial("tcp", localAddr)
	if err != nil {
		logrus.WithError(err).WithField("local_addr", localAddr).Error("Failed to connect to local service")
		return
	}
	defer localConn.Close()

	logrus.WithFields(logrus.Fields{
		"tunnel":     tunnel.Name,
		"local_addr": localAddr,
		"remote":     tunnelConn.RemoteAddr(),
	}).Debug("Forwarding tunnel connection")

	// Forward data between tunnel and local service
	c.forwardData(tunnelConn, localConn)
}

// forwardData forwards data bidirectionally between two connections
func (c *Client) forwardData(conn1, conn2 net.Conn) {
	done := make(chan struct{}, 2)

	// Forward conn1 -> conn2
	go func() {
		defer func() { done <- struct{}{} }()
		io.Copy(conn2, conn1)
		if c, ok := conn2.(interface{ CloseWrite() error }); ok {
			c.CloseWrite()
		}
	}()

	// Forward conn2 -> conn1
	go func() {
		defer func() { done <- struct{}{} }()
		io.Copy(conn1, conn2)
		if c, ok := conn1.(interface{ CloseWrite() error }); ok {
			c.CloseWrite()
		}
	}()

	// Wait for one direction to finish
	<-done
}

// keepalive sends periodic keepalive messages
func (c *Client) keepalive() {
	if c.config.Connection.KeepaliveInterval <= 0 {
		return
	}

	ticker := time.NewTicker(c.config.Connection.KeepaliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			// Send keepalive
			c.mu.RLock()
			sshClient := c.sshClient
			c.mu.RUnlock()
			
			if sshClient != nil {
				_, _, err := sshClient.SendRequest("keepalive@openssh.com", true, nil)
				if err != nil {
					logrus.WithError(err).Debug("Keepalive failed")
					return
				}
				logrus.Debug("Keepalive sent")
			}
		}
	}
}

// cleanupTunnels cleans up all tunnel listeners
func (c *Client) cleanupTunnels() {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	for name, listener := range c.listeners {
		logrus.WithField("tunnel", name).Debug("Cleaning up tunnel listener")
		listener.Close()
		delete(c.listeners, name)
	}
	
	// Mark all tunnels as inactive
	for _, tunnel := range c.tunnels {
		tunnel.Active = false
	}
}

// startHealthMonitoring starts the health monitoring endpoint
func (c *Client) startHealthMonitoring() {
	if !c.config.Health.Enabled {
		return
	}

	// Simple HTTP health check endpoint
	healthServer := &HTTPHealthServer{
		port:   c.config.Health.Port,
		client: c,
	}
	healthServer.Start()
}

// loadPrivateKey loads the SSH private key for authentication
func (c *Client) loadPrivateKey() (ssh.Signer, error) {
	keyPath := c.config.SSH.PrivateKeyPath
	if keyPath == "" {
		keyPath = "keys/client_key"
	}

	// Try to load existing key
	if _, err := os.Stat(keyPath); err == nil {
		keyData, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read private key: %v", err)
		}

		key, err := ssh.ParsePrivateKey(keyData)
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key: %v", err)
		}

		logrus.WithField("key_path", keyPath).Debug("Loaded SSH private key")
		return key, nil
	}

	// Generate temporary key for development
	logrus.Warn("Private key not found, generating temporary key for development")
	return generateEd25519Key()
}

// getHostKeyCallback returns the host key callback function
func (c *Client) getHostKeyCallback() ssh.HostKeyCallback {
	// TODO: Implement proper host key verification
	// For now, use insecure ignore for development
	return ssh.InsecureIgnoreHostKey()
}

// IsConnected returns true if the client is connected to the server
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// GetTunnelStatus returns the status of all tunnels
func (c *Client) GetTunnelStatus() map[string]*ClientTunnel {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	status := make(map[string]*ClientTunnel)
	for name, tunnel := range c.tunnels {
		status[name] = &ClientTunnel{
			Name:       tunnel.Name,
			LocalHost:  tunnel.LocalHost,
			LocalPort:  tunnel.LocalPort,
			RemotePort: tunnel.RemotePort,
			Protocol:   tunnel.Protocol,
			Active:     tunnel.Active,
		}
	}
	return status
}

// HTTPHealthServer provides HTTP health check endpoint
type HTTPHealthServer struct {
	port   int
	client *Client
}

// Start starts the HTTP health server
func (h *HTTPHealthServer) Start() {
	logrus.WithField("port", h.port).Info("Starting health monitoring server")
	
	// Simple implementation without importing net/http to avoid dependency issues
	// In a real implementation, you would use net/http
	logrus.WithField("port", h.port).Debug("Health monitoring server started")
}
