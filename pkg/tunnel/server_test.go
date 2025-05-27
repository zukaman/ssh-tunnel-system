package tunnel

import (
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"ssh-tunnel-system/pkg/config"

	"golang.org/x/crypto/ssh"
)

// TestServer is a helper struct for testing
type TestServer struct {
	server     *Server
	config     *config.ServerConfig
	clientKeys []ssh.Signer
	tempDir    string
}

// setupTestServer creates a test server with temporary files
func setupTestServer(t *testing.T) *TestServer {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "ssh-tunnel-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	// Create test configuration
	cfg := &config.ServerConfig{
		Server: config.ServerSection{
			SSHHost:            "127.0.0.1",
			SSHPort:            0, // Will be set after listener starts
			HostKeyPath:        filepath.Join(tempDir, "host_key"),
			AuthorizedKeysPath: filepath.Join(tempDir, "authorized_keys"),
			PortRange: config.PortRange{
				Start: 12000,
				End:   12099,
			},
		},
		Logging: config.LoggingConfig{
			Level:  "debug",
			Format: "text",
		},
	}

	// Generate test client keys
	clientKeys := make([]ssh.Signer, 3)
	authorizedKeysData := ""

	for i := 0; i < 3; i++ {
		// Generate RSA key for testing
		rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("Failed to generate RSA key: %v", err)
		}

		signer, err := ssh.NewSignerFromKey(rsaKey)
		if err != nil {
			t.Fatalf("Failed to create signer: %v", err)
		}

		clientKeys[i] = signer

		// Add to authorized keys
		pubKey := signer.PublicKey()
		authorizedKeysData += fmt.Sprintf("%s test-client-%d\n", 
			string(ssh.MarshalAuthorizedKey(pubKey)), i)
	}

	// Write authorized keys file
	err = os.WriteFile(cfg.Server.AuthorizedKeysPath, []byte(authorizedKeysData), 0600)
	if err != nil {
		t.Fatalf("Failed to write authorized keys: %v", err)
	}

	// Create server
	server, err := NewServer(cfg)
	if err != nil {
		os.RemoveAll(tempDir)
		t.Fatalf("Failed to create server: %v", err)
	}

	return &TestServer{
		server:     server,
		config:     cfg,
		clientKeys: clientKeys,
		tempDir:    tempDir,
	}
}

// cleanup cleans up test resources
func (ts *TestServer) cleanup() {
	if ts.server != nil {
		ts.server.Stop()
	}
	os.RemoveAll(ts.tempDir)
}

// startServer starts the test server and returns the actual port
func (ts *TestServer) startServer(t *testing.T) int {
	// Start server
	err := ts.server.Start()
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	// Get the actual port from the listener
	addr := ts.server.listener.Addr().(*net.TCPAddr)
	port := addr.Port
	ts.config.Server.SSHPort = port

	return port
}

// createSSHClient creates an SSH client connection
func (ts *TestServer) createSSHClient(t *testing.T, keyIndex int, username string) *ssh.Client {
	if keyIndex >= len(ts.clientKeys) {
		t.Fatalf("Invalid key index: %d", keyIndex)
	}

	sshConfig := &ssh.ClientConfig{
		User: username,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(ts.clientKeys[keyIndex]),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	addr := fmt.Sprintf("127.0.0.1:%d", ts.config.Server.SSHPort)
	client, err := ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		t.Fatalf("Failed to connect SSH client: %v", err)
	}

	return client
}

func TestServerCreation(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.cleanup()

	// Test server creation
	if ts.server == nil {
		t.Fatal("Server should not be nil")
	}

	// Test configuration loading
	if ts.server.config.Server.SSHHost != "127.0.0.1" {
		t.Errorf("Expected SSH host 127.0.0.1, got %s", ts.server.config.Server.SSHHost)
	}

	// Test port pool initialization
	if ts.server.portPool == nil {
		t.Fatal("Port pool should not be nil")
	}

	if len(ts.server.portPool.available) != 100 {
		t.Errorf("Expected 100 available ports, got %d", len(ts.server.portPool.available))
	}
}

func TestServerStartStop(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.cleanup()

	// Test server start
	port := ts.startServer(t)
	if port == 0 {
		t.Fatal("Server should start and return valid port")
	}

	// Test connection
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
	if err != nil {
		t.Fatalf("Should be able to connect to server: %v", err)
	}
	conn.Close()

	// Test server stop
	ts.server.Stop()

	// Should not be able to connect after stop
	time.Sleep(100 * time.Millisecond)
	_, err = net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 1*time.Second)
	if err == nil {
		t.Fatal("Should not be able to connect after server stop")
	}
}

func TestSSHAuthentication(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.cleanup()

	ts.startServer(t)

	// Test successful authentication
	client := ts.createSSHClient(t, 0, "test-user-1")
	defer client.Close()

	// Test connection is working
	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	session.Close()
}

func TestSSHAuthenticationFailure(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.cleanup()

	ts.startServer(t)

	// Generate unauthorized key
	unauthorizedKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate unauthorized key: %v", err)
	}

	signer, err := ssh.NewSignerFromKey(unauthorizedKey)
	if err != nil {
		t.Fatalf("Failed to create signer: %v", err)
	}

	// Try to connect with unauthorized key
	sshConfig := &ssh.ClientConfig{
		User: "unauthorized-user",
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         2 * time.Second,
	}

	addr := fmt.Sprintf("127.0.0.1:%d", ts.config.Server.SSHPort)
	_, err = ssh.Dial("tcp", addr, sshConfig)
	if err == nil {
		t.Fatal("Should fail to connect with unauthorized key")
	}
}

func TestPortAllocation(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.cleanup()

	portPool := ts.server.portPool

	// Test initial state
	initialAvailable := len(portPool.available)
	if initialAvailable != 100 {
		t.Errorf("Expected 100 available ports, got %d", initialAvailable)
	}

	// Allocate ports
	var allocatedPorts []int
	for i := 0; i < 5; i++ {
		port, err := portPool.AllocatePort()
		if err != nil {
			t.Fatalf("Failed to allocate port %d: %v", i, err)
		}

		if port < 12000 || port > 12099 {
			t.Errorf("Allocated port %d out of range [12000-12099]", port)
		}

		allocatedPorts = append(allocatedPorts, port)
	}

	// Check available count decreased
	if len(portPool.available) != initialAvailable-5 {
		t.Errorf("Expected %d available ports, got %d", initialAvailable-5, len(portPool.available))
	}

	// Release ports
	for _, port := range allocatedPorts {
		portPool.ReleasePort(port)
	}

	// Check available count restored
	if len(portPool.available) != initialAvailable {
		t.Errorf("Expected %d available ports after release, got %d", initialAvailable, len(portPool.available))
	}
}

func TestReverseTunnel(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.cleanup()

	ts.startServer(t)

	// Create SSH client
	client := ts.createSSHClient(t, 0, "test-user")
	defer client.Close()

	// Start a simple HTTP server on a random port to tunnel to
	testServer := startTestHTTPServer(t)
	defer testServer.Close()

	// Request reverse tunnel
	remoteListener, err := client.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create remote listener: %v", err)
	}
	defer remoteListener.Close()

	// Get the assigned port
	assignedPort := remoteListener.Addr().(*net.TCPAddr).Port

	// Handle connections from tunnel to test server
	go func() {
		for {
			conn, err := remoteListener.Accept()
			if err != nil {
				return
			}

			// Connect to test HTTP server
			go func(conn net.Conn) {
				defer conn.Close()
				
				targetConn, err := net.Dial("tcp", testServer.Addr)
				if err != nil {
					return
				}
				defer targetConn.Close()

				// Forward data bidirectionally
				var wg sync.WaitGroup
				wg.Add(2)

				go func() {
					defer wg.Done()
					io.Copy(targetConn, conn)
				}()

				go func() {
					defer wg.Done()
					io.Copy(conn, targetConn)
				}()

				wg.Wait()
			}(conn)
		}
	}()

	// Test connection through tunnel
	time.Sleep(100 * time.Millisecond) // Let the tunnel establish

	// Connect to the tunnel
	tunnelConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", assignedPort))
	if err != nil {
		t.Fatalf("Failed to connect through tunnel: %v", err)
	}
	defer tunnelConn.Close()

	// Send HTTP request
	request := "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n"
	_, err = tunnelConn.Write([]byte(request))
	if err != nil {
		t.Fatalf("Failed to write request: %v", err)
	}

	// Read response
	buffer := make([]byte, 1024)
	n, err := tunnelConn.Read(buffer)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	response := string(buffer[:n])
	if !contains(response, "HTTP/1.1 200 OK") {
		t.Errorf("Expected HTTP 200 OK response, got: %s", response)
	}
}

func TestMultipleClients(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.cleanup()

	ts.startServer(t)

	// Connect multiple clients
	var clients []*ssh.Client
	for i := 0; i < 3; i++ {
		client := ts.createSSHClient(t, i%len(ts.clientKeys), fmt.Sprintf("test-user-%d", i))
		clients = append(clients, client)
	}

	// Clean up clients
	defer func() {
		for _, client := range clients {
			client.Close()
		}
	}()

	// Check server statistics
	stats := ts.server.GetClientStats()
	if stats["total_clients"].(int) != 3 {
		t.Errorf("Expected 3 connected clients, got %d", stats["total_clients"].(int))
	}

	// Close one client
	clients[0].Close()
	time.Sleep(100 * time.Millisecond) // Allow cleanup

	// Check updated statistics
	stats = ts.server.GetClientStats()
	if stats["total_clients"].(int) != 2 {
		t.Errorf("Expected 2 connected clients after disconnect, got %d", stats["total_clients"].(int))
	}
}

func TestPortExhaustion(t *testing.T) {
	// Create server with very small port range
	tempDir, err := os.MkdirTemp("", "ssh-tunnel-test-exhaustion-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cfg := &config.ServerConfig{
		Server: config.ServerSection{
			SSHHost:     "127.0.0.1",
			SSHPort:     0,
			HostKeyPath: filepath.Join(tempDir, "host_key"),
			PortRange: config.PortRange{
				Start: 13000,
				End:   13002, // Only 3 ports available
			},
		},
		Logging: config.LoggingConfig{
			Level: "debug",
		},
	}

	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Stop()

	// Allocate all available ports
	var ports []int
	for i := 0; i < 3; i++ {
		port, err := server.portPool.AllocatePort()
		if err != nil {
			t.Fatalf("Failed to allocate port %d: %v", i, err)
		}
		ports = append(ports, port)
	}

	// Try to allocate one more - should fail
	_, err = server.portPool.AllocatePort()
	if err == nil {
		t.Fatal("Should fail to allocate port when pool is exhausted")
	}

	// Release one port
	server.portPool.ReleasePort(ports[0])

	// Should be able to allocate again
	_, err = server.portPool.AllocatePort()
	if err != nil {
		t.Fatalf("Should be able to allocate after releasing port: %v", err)
	}
}

// Helper functions

// startTestHTTPServer starts a simple HTTP server for testing
func startTestHTTPServer(t *testing.T) *TestHTTPServer {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start test HTTP server: %v", err)
	}

	server := &TestHTTPServer{
		Addr:     listener.Addr().String(),
		listener: listener,
	}

	go server.serve()
	return server
}

type TestHTTPServer struct {
	Addr     string
	listener net.Listener
}

func (s *TestHTTPServer) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}

		go func(conn net.Conn) {
			defer conn.Close()
			
			// Read request (simple implementation)
			buffer := make([]byte, 1024)
			_, err := conn.Read(buffer)
			if err != nil {
				return
			}

			// Send simple HTTP response
			response := "HTTP/1.1 200 OK\r\nContent-Length: 13\r\n\r\nHello, World!"
			conn.Write([]byte(response))
		}(conn)
	}
}

func (s *TestHTTPServer) Close() {
	if s.listener != nil {
		s.listener.Close()
	}
}

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && 
		   (s == substr || 
		    contains(s[1:], substr) || 
		    (len(s) > 0 && s[:len(substr)] == substr))
}
