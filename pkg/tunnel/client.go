package tunnel

import (
	"fmt"
	"io"
	"net"
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
	mu         sync.RWMutex
	done       chan struct{}
	reconnect  chan struct{}
}

// ClientTunnel represents a client-side tunnel
type ClientTunnel struct {
	Name      string
	LocalHost string
	LocalPort int
	Protocol  string
	Active    bool
}

// NewClient creates a new tunnel client
func NewClient(config *config.ClientConfig) (*Client, error) {
	client := &Client{
		config:    config,
		tunnels:   make(map[string]*ClientTunnel),
		done:      make(chan struct{}),
		reconnect: make(chan struct{}, 1),
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
	if c.sshClient != nil {
		c.sshClient.Close()
	}
	if c.conn != nil {
		c.conn.Close()
	}
	c.mu.Unlock()
}

// connectionLoop manages the connection to the tunnel server
func (c *Client) connectionLoop() {
	retryCount := 0

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
			if c.config.Connection.MaxRetries > 0 && retryCount >= c.config.Connection.MaxRetries {
				logrus.Error("Maximum retry attempts reached, giving up")
				return
			}
			
			retryCount++
			logrus.WithField("retry_count", retryCount).Info("Retrying connection")
			
			// Wait before retrying
			select {
			case <-c.done:
				return
			case <-time.After(c.config.Connection.RetryInterval):
			}
			continue
		}

		// Reset retry count on successful connection
		retryCount = 0
		
		// Handle the connection
		c.handleConnection()
		
		// If we get here, connection was lost
		logrus.Warn("Connection lost, attempting to reconnect")
		
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
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // TODO: Implement proper host key verification
		Timeout:         c.config.Connection.ConnectTimeout,
	}

	// Connect to server
	address := fmt.Sprintf("%s:%d", c.config.Server.Host, c.config.Server.Port)
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

	logrus.WithField("server", address).Info("Connected to tunnel server")
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
		return
	}
}

// setupTunnels sets up reverse tunnels for all configured services
func (c *Client) setupTunnels() error {
	for name, tunnel := range c.tunnels {
		logrus.WithFields(logrus.Fields{
			"tunnel": name,
			"local":  fmt.Sprintf("%s:%d", tunnel.LocalHost, tunnel.LocalPort),
		}).Info("Setting up tunnel")

		// For SSH tunnels, we'll use reverse port forwarding
		if tunnel.Protocol == "tcp" || tunnel.Protocol == "ssh" {
			if err := c.setupReverseTunnel(tunnel); err != nil {
				logrus.WithError(err).WithField("tunnel", name).Error("Failed to setup reverse tunnel")
				continue
			}
		}

		tunnel.Active = true
	}

	return nil
}

// setupReverseTunnel sets up a reverse TCP tunnel
func (c *Client) setupReverseTunnel(tunnel *ClientTunnel) error {
	// Request reverse port forwarding
	// The server will listen on a port and forward connections to us
	listener, err := c.sshClient.Listen("tcp", fmt.Sprintf("0.0.0.0:0")) // 0 means server assigns port
	if err != nil {
		return fmt.Errorf("failed to setup reverse tunnel: %v", err)
	}

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
	}()

	// Forward conn2 -> conn1
	go func() {
		defer func() { done <- struct{}{} }()
		io.Copy(conn1, conn2)
	}()

	// Wait for one direction to finish
	<-done
}

// keepalive sends periodic keepalive messages
func (c *Client) keepalive() {
	ticker := time.NewTicker(c.config.Connection.KeepaliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			// Send keepalive
			if c.sshClient != nil {
				_, _, err := c.sshClient.SendRequest("keepalive@openssh.com", true, nil)
				if err != nil {
					logrus.WithError(err).Debug("Keepalive failed")
					return
				}
			}
		}
	}
}

// startHealthMonitoring starts the health monitoring endpoint
func (c *Client) startHealthMonitoring() {
	if !c.config.Health.Enabled {
		return
	}

	// TODO: Implement health monitoring HTTP endpoint
	logrus.WithField("port", c.config.Health.Port).Info("Health monitoring enabled")
}

// loadPrivateKey loads the SSH private key for authentication
func (c *Client) loadPrivateKey() (ssh.Signer, error) {
	// TODO: Implement private key loading from file
	// For now, generate a temporary key for development
	return generateEd25519Key()
}
