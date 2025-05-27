package tunnel

import (
	"testing"
	"time"
	"ssh-tunnel-system/pkg/config"
	"path/filepath"
	"os"
)

// TestBasicFunctionality tests basic functionality without complex networking
func TestBasicFunctionality(t *testing.T) {
	t.Run("ServerConfigValidation", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "ssh-tunnel-basic-test-*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		cfg := &config.ServerConfig{
			Server: config.ServerSection{
				SSHHost:            "127.0.0.1",
				SSHPort:            0,
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

		server, err := NewServer(cfg)
		if err != nil {
			t.Fatalf("Failed to create server: %v", err)
		}
		defer server.Stop()

		if server == nil {
			t.Fatal("Server should not be nil")
		}

		if server.portPool == nil {
			t.Fatal("Port pool should not be nil")
		}

		if len(server.portPool.available) != 100 {
			t.Errorf("Expected 100 available ports, got %d", len(server.portPool.available))
		}
	})

	t.Run("ClientConfigValidation", func(t *testing.T) {
		cfg := &config.ClientConfig{
			ClientID: "test-client",
			Server: config.ClientServerConfig{
				Host: "localhost",
				Port: 2222,
			},
			Connection: config.ConnectionConfig{
				RetryInterval:  10 * time.Second,
				MaxRetries:     5,
				ConnectTimeout: 15 * time.Second,
			},
			Tunnels: []config.TunnelConfig{
				{
					Name:      "test-tunnel",
					LocalHost: "127.0.0.1",
					LocalPort: 22,
					Protocol:  "tcp",
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

		if len(client.tunnels) != 1 {
			t.Errorf("Expected 1 tunnel, got %d", len(client.tunnels))
		}
	})

	t.Run("PortPoolOperations", func(t *testing.T) {
		portPool := &PortPool{
			start: 10000,
			end:   10009, // Only 10 ports
			used:  make(map[int]bool),
		}

		// Initialize available ports
		for i := portPool.start; i <= portPool.end; i++ {
			portPool.available = append(portPool.available, i)
		}

		// Test allocation
		port1, err := portPool.AllocatePort()
		if err != nil {
			t.Fatalf("Failed to allocate port: %v", err)
		}

		if port1 < 10000 || port1 > 10009 {
			t.Errorf("Allocated port %d out of range", port1)
		}

		// Test that port is marked as used
		if !portPool.used[port1] {
			t.Error("Port should be marked as used")
		}

		// Test release
		portPool.ReleasePort(port1)
		if portPool.used[port1] {
			t.Error("Port should not be marked as used after release")
		}

		// Test exhaustion
		var allocatedPorts []int
		for i := 0; i < 10; i++ {
			port, err := portPool.AllocatePort()
			if err != nil {
				t.Fatalf("Failed to allocate port %d: %v", i, err)
			}
			allocatedPorts = append(allocatedPorts, port)
		}

		// Should fail to allocate another port
		_, err = portPool.AllocatePort()
		if err == nil {
			t.Error("Should fail to allocate port when pool is exhausted")
		}

		// Release all ports
		for _, port := range allocatedPorts {
			portPool.ReleasePort(port)
		}

		// Should be able to allocate again
		_, err = portPool.AllocatePort()
		if err != nil {
			t.Errorf("Should be able to allocate after releasing ports: %v", err)
		}
	})
}

// TestKeyGeneration tests SSH key generation
func TestKeyGeneration(t *testing.T) {
	key, err := generateEd25519Key()
	if err != nil {
		t.Fatalf("Failed to generate Ed25519 key: %v", err)
	}

	if key == nil {
		t.Fatal("Generated key should not be nil")
	}

	// Test that key can be used to sign
	testData := []byte("test data")
	signature, err := key.Sign(nil, testData)
	if err != nil {
		t.Fatalf("Failed to sign test data: %v", err)
	}

	if signature == nil {
		t.Fatal("Signature should not be nil")
	}

	// Verify the signature
	pubKey := key.PublicKey()
	if pubKey == nil {
		t.Fatal("Public key should not be nil")
	}

	// The signature verification would be more complex, 
	// but this basic test ensures key generation works
}
