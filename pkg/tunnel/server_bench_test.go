package tunnel

import (
	"bytes"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// BenchmarkServerCreation benchmarks server creation
func BenchmarkServerCreation(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ts := setupTestServer(&testing.T{})
		ts.cleanup()
	}
}

// BenchmarkPortAllocation benchmarks port allocation/release
func BenchmarkPortAllocation(b *testing.B) {
	ts := setupTestServer(&testing.T{})
	defer ts.cleanup()

	portPool := ts.server.portPool

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		port, _ := portPool.AllocatePort()
		portPool.ReleasePort(port)
	}
}

// BenchmarkSSHConnections benchmarks SSH connection establishment
func BenchmarkSSHConnections(b *testing.B) {
	ts := setupTestServer(&testing.T{})
	defer ts.cleanup()

	ts.startServer(&testing.T{})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		client := ts.createSSHClient(&testing.T{}, 0, fmt.Sprintf("bench-user-%d", i))
		client.Close()
	}
}

// BenchmarkReverseTunnelThroughput benchmarks data throughput through reverse tunnel
func BenchmarkReverseTunnelThroughput(b *testing.B) {
	ts := setupTestServer(&testing.T{})
	defer ts.cleanup()

	ts.startServer(&testing.T{})

	// Create SSH client
	client := ts.createSSHClient(&testing.T{}, 0, "bench-user")
	defer client.Close()

	// Create reverse tunnel
	remoteListener, err := client.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatalf("Failed to create remote listener: %v", err)
	}
	defer remoteListener.Close()

	assignedPort := remoteListener.Addr().(*net.TCPAddr).Port

	// Start echo server on the "remote" side
	go func() {
		for {
			conn, err := remoteListener.Accept()
			if err != nil {
				return
			}

			go func(conn net.Conn) {
				defer conn.Close()
				buf := make([]byte, 1024)
				for {
					n, err := conn.Read(buf)
					if err != nil {
						return
					}
					conn.Write(buf[:n])
				}
			}(conn)
		}
	}()

	// Wait for tunnel to be ready
	time.Sleep(100 * time.Millisecond)

	// Benchmark data transfer
	testData := make([]byte, 1024)
	for i := range testData {
		testData[i] = byte(i % 256)
	}

	b.ResetTimer()
	b.SetBytes(int64(len(testData)))

	for i := 0; i < b.N; i++ {
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", assignedPort))
		if err != nil {
			b.Fatalf("Failed to connect: %v", err)
		}

		// Send data
		_, err = conn.Write(testData)
		if err != nil {
			b.Fatalf("Failed to write: %v", err)
		}

		// Read echo
		received := make([]byte, len(testData))
		_, err = conn.Read(received)
		if err != nil {
			b.Fatalf("Failed to read: %v", err)
		}

		conn.Close()

		if !bytes.Equal(testData, received) {
			b.Fatal("Data corruption detected")
		}
	}
}

// BenchmarkConcurrentClients benchmarks multiple concurrent clients
func BenchmarkConcurrentClients(b *testing.B) {
	ts := setupTestServer(&testing.T{})
	defer ts.cleanup()

	ts.startServer(&testing.T{})

	concurrency := 10
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		wg.Add(concurrency)

		for j := 0; j < concurrency; j++ {
			go func(clientID int) {
				defer wg.Done()
				
				client := ts.createSSHClient(&testing.T{}, clientID%len(ts.clientKeys), 
					fmt.Sprintf("concurrent-user-%d", clientID))
				defer client.Close()

				// Create a session to simulate work
				session, err := client.NewSession()
				if err != nil {
					b.Errorf("Failed to create session: %v", err)
					return
				}
				session.Close()
			}(j)
		}

		wg.Wait()
	}
}

// TestTunnelReliability tests tunnel reliability under various conditions
func TestTunnelReliability(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.cleanup()

	ts.startServer(t)

	// Create SSH client
	client := ts.createSSHClient(t, 0, "reliability-test")
	defer client.Close()

	// Create reverse tunnel
	remoteListener, err := client.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create remote listener: %v", err)
	}
	defer remoteListener.Close()

	assignedPort := remoteListener.Addr().(*net.TCPAddr).Port

	// Start a simple echo server
	go func() {
		for {
			conn, err := remoteListener.Accept()
			if err != nil {
				return
			}

			go func(conn net.Conn) {
				defer conn.Close()
				buf := make([]byte, 1024)
				for {
					n, err := conn.Read(buf)
					if err != nil {
						return
					}
					_, err = conn.Write(buf[:n])
					if err != nil {
						return
					}
				}
			}(conn)
		}
	}()

	// Wait for tunnel to be ready
	time.Sleep(100 * time.Millisecond)

	// Test multiple concurrent connections through tunnel
	concurrency := 5
	iterations := 10
	var wg sync.WaitGroup

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()

			for j := 0; j < iterations; j++ {
				conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", assignedPort))
				if err != nil {
					t.Errorf("Client %d iteration %d: failed to connect: %v", clientID, j, err)
					continue
				}

				// Send test data
				testMsg := fmt.Sprintf("Hello from client %d iteration %d", clientID, j)
				_, err = conn.Write([]byte(testMsg))
				if err != nil {
					t.Errorf("Client %d iteration %d: failed to write: %v", clientID, j, err)
					conn.Close()
					continue
				}

				// Read echo
				buf := make([]byte, len(testMsg))
				n, err := conn.Read(buf)
				if err != nil {
					t.Errorf("Client %d iteration %d: failed to read: %v", clientID, j, err)
					conn.Close()
					continue
				}

				received := string(buf[:n])
				if received != testMsg {
					t.Errorf("Client %d iteration %d: expected %q, got %q", 
						clientID, j, testMsg, received)
				}

				conn.Close()
			}
		}(i)
	}

	wg.Wait()
}

// TestServerRecovery tests server recovery from various failure scenarios
func TestServerRecovery(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.cleanup()

	ts.startServer(t)

	// Test 1: Multiple rapid client connections and disconnections
	t.Run("RapidConnections", func(t *testing.T) {
		var clients []*ssh.Client
		
		// Connect multiple clients rapidly
		for i := 0; i < 10; i++ {
			client := ts.createSSHClient(t, i%len(ts.clientKeys), fmt.Sprintf("rapid-client-%d", i))
			clients = append(clients, client)
			time.Sleep(10 * time.Millisecond) // Small delay
		}

		// Disconnect all clients rapidly
		for _, client := range clients {
			client.Close()
		}

		// Wait for cleanup
		time.Sleep(200 * time.Millisecond)

		// Verify server is still functional
		testClient := ts.createSSHClient(t, 0, "recovery-test")
		defer testClient.Close()

		session, err := testClient.NewSession()
		if err != nil {
			t.Fatalf("Server should be functional after rapid connections: %v", err)
		}
		session.Close()
	})

	// Test 2: Port exhaustion and recovery
	t.Run("PortExhaustion", func(t *testing.T) {
		// This is limited by our test setup's port range (100 ports)
		// We'll allocate many ports and then release them
		var ports []int
		
		// Allocate as many ports as possible
		for i := 0; i < 50; i++ { // Don't exhaust completely to allow other tests
			port, err := ts.server.portPool.AllocatePort()
			if err != nil {
				break
			}
			ports = append(ports, port)
		}

		// Release all ports
		for _, port := range ports {
			ts.server.portPool.ReleasePort(port)
		}

		// Verify we can still allocate ports
		port, err := ts.server.portPool.AllocatePort()
		if err != nil {
			t.Fatalf("Should be able to allocate port after release: %v", err)
		}
		ts.server.portPool.ReleasePort(port)
	})
}

// TestLargeDataTransfer tests transferring large amounts of data through tunnel
func TestLargeDataTransfer(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.cleanup()

	ts.startServer(t)

	// Create SSH client
	client := ts.createSSHClient(t, 0, "large-data-test")
	defer client.Close()

	// Create reverse tunnel
	remoteListener, err := client.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create remote listener: %v", err)
	}
	defer remoteListener.Close()

	assignedPort := remoteListener.Addr().(*net.TCPAddr).Port

	// Start echo server
	go func() {
		for {
			conn, err := remoteListener.Accept()
			if err != nil {
				return
			}

			go func(conn net.Conn) {
				defer conn.Close()
				buf := make([]byte, 32*1024) // 32KB buffer
				for {
					n, err := conn.Read(buf)
					if err != nil {
						return
					}
					_, err = conn.Write(buf[:n])
					if err != nil {
						return
					}
				}
			}(conn)
		}
	}()

	// Wait for tunnel to be ready
	time.Sleep(100 * time.Millisecond)

	// Test with 1MB of data
	dataSize := 1024 * 1024 // 1MB
	testData := make([]byte, dataSize)
	for i := range testData {
		testData[i] = byte(i % 256)
	}

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", assignedPort))
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Send large data
	start := time.Now()
	written, err := conn.Write(testData)
	if err != nil {
		t.Fatalf("Failed to write large data: %v", err)
	}

	if written != len(testData) {
		t.Fatalf("Expected to write %d bytes, wrote %d", len(testData), written)
	}

	// Read echo back
	received := make([]byte, len(testData))
	totalRead := 0
	for totalRead < len(testData) {
		n, err := conn.Read(received[totalRead:])
		if err != nil {
			t.Fatalf("Failed to read large data: %v", err)
		}
		totalRead += n
	}

	duration := time.Since(start)
	throughput := float64(len(testData)*2) / duration.Seconds() / (1024 * 1024) // MB/s

	t.Logf("Transferred %d bytes in %v (%.2f MB/s)", len(testData)*2, duration, throughput)

	// Verify data integrity
	if !bytes.Equal(testData, received) {
		t.Fatal("Data corruption detected in large transfer")
	}
}

// TestTunnelTimeout tests tunnel behavior with connection timeouts
func TestTunnelTimeout(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.cleanup()

	ts.startServer(t)

	// Create SSH client
	client := ts.createSSHClient(t, 0, "timeout-test")
	defer client.Close()

	// Create reverse tunnel
	remoteListener, err := client.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create remote listener: %v", err)
	}
	defer remoteListener.Close()

	assignedPort := remoteListener.Addr().(*net.TCPAddr).Port

	// Start slow server that delays responses
	go func() {
		for {
			conn, err := remoteListener.Accept()
			if err != nil {
				return
			}

			go func(conn net.Conn) {
				defer conn.Close()
				buf := make([]byte, 1024)
				n, err := conn.Read(buf)
				if err != nil {
					return
				}
				
				// Delay response by 2 seconds
				time.Sleep(2 * time.Second)
				conn.Write(buf[:n])
			}(conn)
		}
	}()

	// Wait for tunnel to be ready
	time.Sleep(100 * time.Millisecond)

	// Test with timeout
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", assignedPort))
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Set read timeout
	conn.SetReadDeadline(time.Now().Add(1 * time.Second))

	// Send data
	testMsg := "timeout test"
	_, err = conn.Write([]byte(testMsg))
	if err != nil {
		t.Fatalf("Failed to write: %v", err)
	}

	// Try to read - should timeout
	buf := make([]byte, len(testMsg))
	_, err = conn.Read(buf)
	if err == nil {
		t.Fatal("Expected timeout error, but read succeeded")
	}

	// Verify it's a timeout error
	if netErr, ok := err.(net.Error); !ok || !netErr.Timeout() {
		t.Fatalf("Expected timeout error, got: %v", err)
	}
}

// TestClientReconnection tests client reconnection capabilities
func TestClientReconnection(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.cleanup()

	ts.startServer(t)

	// Test 1: Connect, disconnect, reconnect
	client1 := ts.createSSHClient(t, 0, "reconnect-test")
	
	// Verify initial connection works
	session1, err := client1.NewSession()
	if err != nil {
		t.Fatalf("Failed to create initial session: %v", err)
	}
	session1.Close()

	// Close client
	client1.Close()

	// Wait a moment
	time.Sleep(100 * time.Millisecond)

	// Reconnect with same credentials
	client2 := ts.createSSHClient(t, 0, "reconnect-test")
	defer client2.Close()

	// Verify reconnection works
	session2, err := client2.NewSession()
	if err != nil {
		t.Fatalf("Failed to create session after reconnect: %v", err)
	}
	session2.Close()

	// Verify server statistics are correct
	stats := ts.server.GetClientStats()
	if stats["total_clients"].(int) != 1 {
		t.Errorf("Expected 1 connected client after reconnect, got %d", stats["total_clients"].(int))
	}
}
