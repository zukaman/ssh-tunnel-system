package tunnel

import (
	"fmt"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"ssh-tunnel-system/pkg/config"

	"golang.org/x/crypto/ssh"
)

// TestClientCreation tests client creation and initialization
func TestClientCreation(t *testing.T) {
	cfg := &config.ClientConfig{
		ClientID: "test-client",
		Server: config.ClientServerConfig{
			Host: "localhost",
			Port: 2222,
		},
		SSH: config.SSHConfig{
			PrivateKeyPath: "keys/client_key",
		},
		Connection: config.ConnectionConfig{
			RetryInterval:     10 * time.Second,
			MaxRetries:        5,
			KeepaliveInterval: 30 * time.Second,
			ConnectTimeout:    15 * time.Second,
		},
		Tunnels: []config.TunnelConfig{
			{
				Name:      "ssh-tunnel",
				LocalHost: "127.0.0.1",
				LocalPort: 22,
				Protocol:  "tcp",
			},
			{
				Name:      "web-tunnel",
				LocalHost: "127.0.0.1",
				LocalPort: 80,
				Protocol:  "http",
			},
		},
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	if client == nil {
		t.Fatal("Client should not be nil")
	}

	if client.config.ClientID != "test-client" {
		t.Errorf("Expected client ID 'test-client', got '%s'", client.config.ClientID)
	}

	if len(client.tunnels) != 2 {
		t.Errorf("Expected 2 tunnels, got %d", len(client.tunnels))
	}

	// Check tunnel configuration
	sshTunnel, exists := client.tunnels["ssh-tunnel"]
	if !exists {
		t.Fatal("SSH tunnel should exist")
	}

	if sshTunnel.LocalPort != 22 {
		t.Errorf("Expected SSH tunnel local port 22, got %d", sshTunnel.LocalPort)
	}

	if sshTunnel.Protocol != "tcp" {
		t.Errorf("Expected SSH tunnel protocol 'tcp', got '%s'", sshTunnel.Protocol)
	}
}

// TestClientStartStop tests client start and stop functionality
func TestClientStartStop(t *testing.T) {
	cfg := &config.ClientConfig{
		ClientID: "test-client",
		Server: config.ClientServerConfig{
			Host: "127.0.0.1",
			Port: 9999, // Non-existent server
		},
		Connection: config.ConnectionConfig{
			RetryInterval:  1 * time.Second,
			MaxRetries:     2,
			ConnectTimeout: 1 * time.Second,
		},
		Health: config.HealthConfig{
			Enabled: false, // Disable health monitoring for test
		},
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Start client (will fail to connect but shouldn't crash)
	err = client.Start()
	if err != nil {
		t.Fatalf("Failed to start client: %v", err)
	}

	// Let it try to connect for a bit
	time.Sleep(500 * time.Millisecond)

	// Check that client is not connected (since server doesn't exist)
	if client.IsConnected() {
		t.Error("Client should not be connected to non-existent server")
	}

	// Stop client
	client.Stop()

	// Give it time to clean up
	time.Sleep(100 * time.Millisecond)

	// Should still not be connected
	if client.IsConnected() {
		t.Error("Client should not be connected after stop")
	}
}

// TestClientWithRealServer tests client connection to a real server
func TestClientWithRealServer(t *testing.T) {
	// Setup test server first
	ts := setupTestServer(t)
	defer ts.cleanup()

	serverPort := ts.startServer(t)

	// Create client config
	cfg := &config.ClientConfig{
		ClientID: "integration-test-client",
		Server: config.ClientServerConfig{
			Host: "127.0.0.1",
			Port: serverPort,
		},
		SSH: config.SSHConfig{
			PrivateKeyPath: "", // Will use generated key
		},
		Connection: config.ConnectionConfig{
			RetryInterval:     2 * time.Second,
			MaxRetries:        3,
			KeepaliveInterval: 5 * time.Second,
			ConnectTimeout:    5 * time.Second,
		},
		Health: config.HealthConfig{
			Enabled: false,
		},
		Tunnels: []config.TunnelConfig{
			{
				Name:      "test-service",
				LocalHost: "127.0.0.1",
				LocalPort: 0, // Will be set to test HTTP server port
				Protocol:  "tcp",
			},
		},
	}

	// Start a test HTTP server to tunnel to
	testHTTPServer := startTestHTTPServer(t)
	defer testHTTPServer.Close()

	// Get the test server port
	testPort := testHTTPServer.listener.Addr().(*net.TCPAddr).Port
	cfg.Tunnels[0].LocalPort = testPort

	// Create and start client
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	err = client.Start()
	if err != nil {
		t.Fatalf("Failed to start client: %v", err)
	}
	defer client.Stop()

	// Wait for connection to establish
	timeout := time.After(10 * time.Second)
	tick := time.Tick(100 * time.Millisecond)

	connected := false
	for {
		select {
		case <-timeout:
			t.Fatal("Client failed to connect within timeout")
		case <-tick:
			if client.IsConnected() {
				connected = true
				break
			}
		}
		if connected {
			break
		}
	}

	// Verify client is connected
	if !client.IsConnected() {
		t.Fatal("Client should be connected")
	}

	// Check tunnel status
	tunnels := client.GetTunnelStatus()
	if len(tunnels) != 1 {
		t.Errorf("Expected 1 tunnel, got %d", len(tunnels))
	}

	testTunnel := tunnels["test-service"]
	if testTunnel == nil {
		t.Fatal("Test tunnel should exist")
	}

	if !testTunnel.Active {
		t.Error("Test tunnel should be active")
	}

	if testTunnel.RemotePort == 0 {
		t.Error("Test tunnel should have assigned remote port")
	}

	t.Logf("Tunnel established - Local: %s:%d, Remote port: %d", 
		testTunnel.LocalHost, testTunnel.LocalPort, testTunnel.RemotePort)
}

// TestClientReconnection tests client reconnection functionality
func TestClientReconnection(t *testing.T) {
	// Setup test server
	ts := setupTestServer(t)
	defer ts.cleanup()

	serverPort := ts.startServer(t)

	// Create client config
	cfg := &config.ClientConfig{
		ClientID: "reconnect-test-client",
		Server: config.ClientServerConfig{
			Host: "127.0.0.1",
			Port: serverPort,
		},
		Connection: config.ConnectionConfig{
			RetryInterval:     1 * time.Second,
			MaxRetries:        0, // Unlimited retries
			KeepaliveInterval: 2 * time.Second,
			ConnectTimeout:    3 * time.Second,
		},
		Health: config.HealthConfig{
			Enabled: false,
		},
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	err = client.Start()
	if err != nil {
		t.Fatalf("Failed to start client: %v", err)
	}
	defer client.Stop()

	// Wait for initial connection
	time.Sleep(2 * time.Second)

	if !client.IsConnected() {
		t.Fatal("Client should be initially connected")
	}

	// Stop the server to simulate connection loss
	ts.server.Stop()
	time.Sleep(500 * time.Millisecond)

	// Client should detect disconnection
	if client.IsConnected() {
		t.Error("Client should detect disconnection")
	}

	// Restart server
	ts = setupTestServer(t)
	defer ts.cleanup()
	ts.config.Server.SSHPort = serverPort // Use same port
	ts.startServer(t)

	// Wait for reconnection
	reconnected := false
	for i := 0; i < 10; i++ {
		time.Sleep(500 * time.Millisecond)
		if client.IsConnected() {
			reconnected = true
			break
		}
	}

	if !reconnected {
		t.Error("Client should reconnect after server restart")
	}
}

// TestClientTunnelDataFlow tests data flow through established tunnels
func TestClientTunnelDataFlow(t *testing.T) {
	// This is a more complex integration test
	// Setup test server
	ts := setupTestServer(t)
	defer ts.cleanup()

	serverPort := ts.startServer(t)

	// Start test HTTP server
	testHTTPServer := startTestHTTPServer(t)
	defer testHTTPServer.Close()

	testPort := testHTTPServer.listener.Addr().(*net.TCPAddr).Port

	// Create client config
	cfg := &config.ClientConfig{
		ClientID: "dataflow-test-client",
		Server: config.ClientServerConfig{
			Host: "127.0.0.1",
			Port: serverPort,
		},
		Connection: config.ConnectionConfig{
			RetryInterval:     2 * time.Second,
			MaxRetries:        3,
			KeepaliveInterval: 0, // Disable keepalive for this test
			ConnectTimeout:    5 * time.Second,
		},
		Health: config.HealthConfig{
			Enabled: false,
		},
		Tunnels: []config.TunnelConfig{
			{
				Name:      "http-service",
				LocalHost: "127.0.0.1",
				LocalPort: testPort,
				Protocol:  "tcp",
			},
		},
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	err = client.Start()
	if err != nil {
		t.Fatalf("Failed to start client: %v", err)
	}
	defer client.Stop()

	// Wait for connection and tunnel establishment
	time.Sleep(3 * time.Second)

	if !client.IsConnected() {
		t.Fatal("Client should be connected")
	}

	// Get tunnel status
	tunnels := client.GetTunnelStatus()
	httpTunnel := tunnels["http-service"]
	if httpTunnel == nil || !httpTunnel.Active {
		t.Fatal("HTTP tunnel should be active")
	}

	t.Logf("HTTP tunnel established on remote port %d", httpTunnel.RemotePort)

	// Test data flow through tunnel
	// Connect to the remote port on server
	tunnelAddr := fmt.Sprintf("127.0.0.1:%d", httpTunnel.RemotePort)
	tunnelConn, err := net.Dial("tcp", tunnelAddr)
	if err != nil {
		t.Fatalf("Failed to connect to tunnel: %v", err)
	}
	defer tunnelConn.Close()

	// Send HTTP request through tunnel
	request := "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n"
	_, err = tunnelConn.Write([]byte(request))
	if err != nil {
		t.Fatalf("Failed to write request through tunnel: %v", err)
	}

	// Read response
	buffer := make([]byte, 1024)
	n, err := tunnelConn.Read(buffer)
	if err != nil {
		t.Fatalf("Failed to read response through tunnel: %v", err)
	}

	response := string(buffer[:n])
	if !contains(response, "HTTP/1.1 200 OK") {
		t.Errorf("Expected HTTP 200 response through tunnel, got: %s", response)
	}

	if !contains(response, "Hello, World!") {
		t.Errorf("Expected 'Hello, World!' in response, got: %s", response)
	}

	t.Log("Data flow through tunnel verified successfully")
}

// TestClientMultipleTunnels tests client with multiple simultaneous tunnels
func TestClientMultipleTunnels(t *testing.T) {
	// Setup test server
	ts := setupTestServer(t)
	defer ts.cleanup()

	serverPort := ts.startServer(t)

	// Start multiple test services
	httpServer1 := startTestHTTPServer(t)
	defer httpServer1.Close()
	port1 := httpServer1.listener.Addr().(*net.TCPAddr).Port

	httpServer2 := startTestHTTPServer(t)
	defer httpServer2.Close()
	port2 := httpServer2.listener.Addr().(*net.TCPAddr).Port

	// Create client config with multiple tunnels
	cfg := &config.ClientConfig{
		ClientID: "multi-tunnel-client",
		Server: config.ClientServerConfig{
			Host: "127.0.0.1",
			Port: serverPort,
		},
		Connection: config.ConnectionConfig{
			RetryInterval:  2 * time.Second,
			MaxRetries:     3,
			ConnectTimeout: 5 * time.Second,
		},
		Health: config.HealthConfig{
			Enabled: false,
		},
		Tunnels: []config.TunnelConfig{
			{
				Name:      "service-1",
				LocalHost: "127.0.0.1",
				LocalPort: port1,
				Protocol:  "tcp",
			},
			{
				Name:      "service-2",
				LocalHost: "127.0.0.1",
				LocalPort: port2,
				Protocol:  "tcp",
			},
		},
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	err = client.Start()
	if err != nil {
		t.Fatalf("Failed to start client: %v", err)
	}
	defer client.Stop()

	// Wait for tunnels to establish
	time.Sleep(3 * time.Second)

	if !client.IsConnected() {
		t.Fatal("Client should be connected")
	}

	// Verify both tunnels are active
	tunnels := client.GetTunnelStatus()
	if len(tunnels) != 2 {
		t.Fatalf("Expected 2 tunnels, got %d", len(tunnels))
	}

	service1 := tunnels["service-1"]
	service2 := tunnels["service-2"]

	if service1 == nil || !service1.Active {
		t.Error("Service 1 tunnel should be active")
	}

	if service2 == nil || !service2.Active {
		t.Error("Service 2 tunnel should be active")
	}

	if service1.RemotePort == service2.RemotePort {
		t.Error("Services should have different remote ports")
	}

	t.Logf("Multiple tunnels established: Service1 port %d, Service2 port %d",
		service1.RemotePort, service2.RemotePort)
}

// BenchmarkClientConnection benchmarks client connection establishment
func BenchmarkClientConnection(b *testing.B) {
	// Setup test server
	ts := setupTestServer(&testing.T{})
	defer ts.cleanup()

	serverPort := ts.startServer(&testing.T{})

	cfg := &config.ClientConfig{
		ClientID: "benchmark-client",
		Server: config.ClientServerConfig{
			Host: "127.0.0.1",
			Port: serverPort,
		},
		Connection: config.ConnectionConfig{
			RetryInterval:  1 * time.Second,
			MaxRetries:     1,
			ConnectTimeout: 3 * time.Second,
		},
		Health: config.HealthConfig{
			Enabled: false,
		},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		client, err := NewClient(cfg)
		if err != nil {
			b.Fatalf("Failed to create client: %v", err)
		}

		err = client.Start()
		if err != nil {
			b.Fatalf("Failed to start client: %v", err)
		}

		// Wait for connection
		time.Sleep(100 * time.Millisecond)

		client.Stop()
		time.Sleep(10 * time.Millisecond)
	}
}

// TestClientErrorHandling tests various error conditions
func TestClientErrorHandling(t *testing.T) {
	t.Run("InvalidServerAddress", func(t *testing.T) {
		cfg := &config.ClientConfig{
			ClientID: "error-test-client",
			Server: config.ClientServerConfig{
				Host: "invalid-hostname-12345",
				Port: 2222,
			},
			Connection: config.ConnectionConfig{
				RetryInterval:  500 * time.Millisecond,
				MaxRetries:     1,
				ConnectTimeout: 1 * time.Second,
			},
			Health: config.HealthConfig{
				Enabled: false,
			},
		}

		client, err := NewClient(cfg)
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}

		err = client.Start()
		if err != nil {
			t.Fatalf("Failed to start client: %v", err)
		}
		defer client.Stop()

		// Should not be able to connect
		time.Sleep(2 * time.Second)
		
		if client.IsConnected() {
			t.Error("Client should not connect to invalid server")
		}
	})

	t.Run("InvalidLocalService", func(t *testing.T) {
		// Setup test server
		ts := setupTestServer(t)
		defer ts.cleanup()
		serverPort := ts.startServer(t)

		cfg := &config.ClientConfig{
			ClientID: "invalid-service-client",
			Server: config.ClientServerConfig{
				Host: "127.0.0.1",
				Port: serverPort,
			},
			Connection: config.ConnectionConfig{
				RetryInterval:  1 * time.Second,
				MaxRetries:     2,
				ConnectTimeout: 3 * time.Second,
			},
			Health: config.HealthConfig{
				Enabled: false,
			},
			Tunnels: []config.TunnelConfig{
				{
					Name:      "invalid-service",
					LocalHost: "127.0.0.1",
					LocalPort: 99999, // Invalid port
					Protocol:  "tcp",
				},
			},
		}

		client, err := NewClient(cfg)
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}

		err = client.Start()
		if err != nil {
			t.Fatalf("Failed to start client: %v", err)
		}
		defer client.Stop()

		// Wait for connection attempt
		time.Sleep(2 * time.Second)

		// Client should connect to server but tunnel should not be active
		tunnels := client.GetTunnelStatus()
		invalidTunnel := tunnels["invalid-service"]
		
		if invalidTunnel != nil && invalidTunnel.Active {
			t.Error("Tunnel to invalid service should not be active")
		}
	})
}
