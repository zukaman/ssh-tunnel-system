package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
)

func main() {
	var (
		keyType    = flag.String("type", "ed25519", "Key type (ed25519, rsa)")
		keyPath    = flag.String("path", "", "Path for private key (public key gets .pub suffix)")
		comment    = flag.String("comment", "", "Comment for public key")
		force      = flag.Bool("force", false, "Overwrite existing keys")
		genServer  = flag.Bool("server", false, "Generate server host key")
		genClient  = flag.Bool("client", false, "Generate client key pair")
		setupDir   = flag.String("setup", "", "Setup directory with all necessary keys")
	)
	flag.Parse()

	if *setupDir != "" {
		if err := setupKeysDirectory(*setupDir, *force); err != nil {
			fmt.Fprintf(os.Stderr, "Error setting up keys directory: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if *keyPath == "" {
		if *genServer {
			*keyPath = "keys/ssh_host_ed25519_key"
		} else if *genClient {
			*keyPath = "keys/client_ed25519_key"
		} else {
			fmt.Fprintf(os.Stderr, "Key path is required\n")
			flag.Usage()
			os.Exit(1)
		}
	}

	if err := generateKeyPair(*keyType, *keyPath, *comment, *force); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating key pair: %v\n", err)
		os.Exit(1)
	}
}

// generateKeyPair generates a new SSH key pair
func generateKeyPair(keyType, keyPath, comment string, overwrite bool) error {
	// Check if files already exist
	if !overwrite {
		if _, err := os.Stat(keyPath); err == nil {
			return fmt.Errorf("private key file already exists: %s (use -force to overwrite)", keyPath)
		}
		if _, err := os.Stat(keyPath + ".pub"); err == nil {
			return fmt.Errorf("public key file already exists: %s.pub (use -force to overwrite)", keyPath)
		}
	}

	// Create directory if it doesn't exist
	keyDir := filepath.Dir(keyPath)
	if err := os.MkdirAll(keyDir, 0700); err != nil {
		return fmt.Errorf("failed to create key directory: %v", err)
	}

	var privateKey interface{}
	var err error

	switch keyType {
	case "ed25519":
		_, privateKey, err = ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return fmt.Errorf("failed to generate Ed25519 key: %v", err)
		}
	default:
		return fmt.Errorf("unsupported key type: %s", keyType)
	}

	// Convert to SSH format
	sshPrivateKey, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		return fmt.Errorf("failed to create SSH signer: %v", err)
	}

	// Generate private key in OpenSSH format
	privateKeyBytes := ssh.MarshalPrivateKey(sshPrivateKey, comment)

	// Write private key
	if err := os.WriteFile(keyPath, privateKeyBytes, 0600); err != nil {
		return fmt.Errorf("failed to write private key: %v", err)
	}

	// Generate public key
	publicKey := sshPrivateKey.PublicKey()
	publicKeyBytes := ssh.MarshalAuthorizedKey(publicKey)

	// Add comment to public key if provided
	if comment != "" {
		publicKeyStr := string(publicKeyBytes)
		publicKeyStr = publicKeyStr[:len(publicKeyStr)-1] + " " + comment + "\n"
		publicKeyBytes = []byte(publicKeyStr)
	}

	// Write public key
	publicKeyPath := keyPath + ".pub"
	if err := os.WriteFile(publicKeyPath, publicKeyBytes, 0644); err != nil {
		return fmt.Errorf("failed to write public key: %v", err)
	}

	fmt.Printf("Generated %s key pair:\n", keyType)
	fmt.Printf("  Private key: %s\n", keyPath)
	fmt.Printf("  Public key:  %s\n", publicKeyPath)
	fmt.Printf("  Fingerprint: %s\n", ssh.FingerprintSHA256(publicKey))

	return nil
}

// setupKeysDirectory sets up a complete keys directory with all necessary files
func setupKeysDirectory(dirPath string, overwrite bool) error {
	fmt.Printf("Setting up SSH keys directory: %s\n", dirPath)

	// Create directory
	if err := os.MkdirAll(dirPath, 0700); err != nil {
		return fmt.Errorf("failed to create directory: %v", err)
	}

	// Generate server host key
	serverKeyPath := filepath.Join(dirPath, "ssh_host_ed25519_key")
	fmt.Println("\n🔐 Generating server host key...")
	if err := generateKeyPair("ed25519", serverKeyPath, "tunnel-server-host-key", overwrite); err != nil {
		return fmt.Errorf("failed to generate server key: %v", err)
	}

	// Generate client key
	clientKeyPath := filepath.Join(dirPath, "client_ed25519_key")
	fmt.Println("\n🔑 Generating client key...")
	if err := generateKeyPair("ed25519", clientKeyPath, "tunnel-client-key", overwrite); err != nil {
		return fmt.Errorf("failed to generate client key: %v", err)
	}

	// Create authorized_keys file with client's public key
	authorizedKeysPath := filepath.Join(dirPath, "authorized_keys")
	if !overwrite {
		if _, err := os.Stat(authorizedKeysPath); err == nil {
			fmt.Printf("📋 authorized_keys file already exists: %s\n", authorizedKeysPath)
		} else {
			if err := createAuthorizedKeysFile(authorizedKeysPath, clientKeyPath+".pub"); err != nil {
				return fmt.Errorf("failed to create authorized_keys: %v", err)
			}
		}
	} else {
		if err := createAuthorizedKeysFile(authorizedKeysPath, clientKeyPath+".pub"); err != nil {
			return fmt.Errorf("failed to create authorized_keys: %v", err)
		}
	}

	// Create example client configuration
	clientConfigPath := filepath.Join(dirPath, "client_config_example.yaml")
	if err := createClientConfigExample(clientConfigPath, clientKeyPath, overwrite); err != nil {
		return fmt.Errorf("failed to create client config example: %v", err)
	}

	fmt.Println("\n✅ SSH keys setup complete!")
	fmt.Println("\nFiles created:")
	fmt.Printf("  🔐 Server host key:    %s\n", serverKeyPath)
	fmt.Printf("  🔐 Server public key:  %s.pub\n", serverKeyPath)
	fmt.Printf("  🔑 Client private key: %s\n", clientKeyPath)
	fmt.Printf("  🔑 Client public key:  %s.pub\n", clientKeyPath)
	fmt.Printf("  📋 Authorized keys:    %s\n", authorizedKeysPath)
	fmt.Printf("  📄 Client config example: %s\n", clientConfigPath)

	fmt.Println("\nNext steps:")
	fmt.Println("1. Copy server keys to your tunnel server")
	fmt.Println("2. Copy client private key to your tunnel clients")
	fmt.Println("3. Configure server to use authorized_keys file")
	fmt.Println("4. Update client configurations with server details")

	return nil
}

// createAuthorizedKeysFile creates authorized_keys file with the given public key
func createAuthorizedKeysFile(authorizedKeysPath, publicKeyPath string) error {
	publicKeyBytes, err := os.ReadFile(publicKeyPath)
	if err != nil {
		return fmt.Errorf("failed to read public key: %v", err)
	}

	// Add header comment
	content := "# SSH Tunnel System - Authorized Keys\n"
	content += "# Add client public keys here, one per line\n\n"
	content += string(publicKeyBytes)

	if err := os.WriteFile(authorizedKeysPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write authorized_keys: %v", err)
	}

	fmt.Printf("📋 Created authorized_keys file: %s\n", authorizedKeysPath)
	return nil
}

// createClientConfigExample creates an example client configuration
func createClientConfigExample(configPath, privateKeyPath string, overwrite bool) error {
	if !overwrite {
		if _, err := os.Stat(configPath); err == nil {
			fmt.Printf("📄 Client config example already exists: %s\n", configPath)
			return nil
		}
	}

	content := `# SSH Tunnel Client Configuration Example
# Copy this to client.yaml and modify as needed

# Server connection settings
server:
  host: "your-tunnel-server.com"  # Change to your server address
  port: 2222
  private_key_path: "` + privateKeyPath + `"

# Client identification
client_id: "client-001"  # Unique identifier for this client

# Tunnels to establish
tunnels:
  - name: "ssh-tunnel"
    local_host: "127.0.0.1"
    local_port: 22        # SSH port on this machine
    remote_port: 0        # 0 = use assigned port from server
    protocol: "tcp"
    type: "reverse"       # reverse = server->client access

  - name: "web-tunnel"
    local_host: "127.0.0.1"
    local_port: 80        # Web server on this machine
    remote_port: 0        # 0 = use assigned port from server
    protocol: "tcp"
    type: "reverse"

# Connection settings
connection:
  retry_interval: "10s"
  max_retries: 0          # 0 = retry forever
  keepalive_interval: "30s"
  keepalive_timeout: "10s"
  connect_timeout: "15s"

# Logging
logging:
  level: "info"
  format: "text"
  file: ""                # empty = stdout

# Health monitoring
health:
  enabled: true
  port: 8081
  report_metrics: true
  metrics_interval: "60s"
`

	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write client config example: %v", err)
	}

	fmt.Printf("📄 Created client config example: %s\n", configPath)
	return nil
}
