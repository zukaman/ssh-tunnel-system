# SSH Tunnel System

[![CI](https://github.com/zukaman/ssh-tunnel-system/actions/workflows/ci.yml/badge.svg)](https://github.com/zukaman/ssh-tunnel-system/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/zukaman/ssh-tunnel-system)](https://goreportcard.com/report/github.com/zukaman/ssh-tunnel-system)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Self-hosted SSH tunnel system (Serveo alternative) for accessing servers behind NAT.

## ⚠️ Development Status

**This project is currently in active development.** While the core SSH tunneling functionality is implemented, extensive testing is still in progress. Use with caution in production environments.

### What's Working ✅
- SSH tunnel server implementation
- Basic reverse tunnel functionality  
- SSH key authentication
- Port allocation and management
- Configuration system
- Build system and CI/CD

### What's Being Tested 🧪
- Client auto-reconnection
- Multi-client scenarios
- Data integrity through tunnels
- Error handling and recovery
- Performance under load

### What's Coming Soon 🚧
- CLI management tools
- Docker containers
- Production deployment guides
- Comprehensive documentation

## Features

- **SSH Reverse Tunnels**: Access servers behind NAT via SSH
- **Auto-reconnection**: Clients automatically reconnect on connection loss (in development)
- **Secure**: SSH key authentication and encrypted connections
- **Lightweight**: Single binary deployment, minimal resource usage
- **Self-hosted**: Full control over your tunnel infrastructure
- **Cross-platform**: Linux, Windows, macOS support

## Architecture

```
[Your Computer] → [Bridge Server] ← [Remote Servers behind NAT]
                       ↕
                 [Management Interface] (coming soon)
```

## Quick Start

### 1. Build from Source

```bash
# Clone repository
git clone https://github.com/zukaman/ssh-tunnel-system.git
cd ssh-tunnel-system

# Quick test to ensure everything compiles
make test-quick

# Build all components
make build
```

### 2. Setup Bridge Server (VPS with public IP)

```bash
# Generate SSH keys
make generate-keys

# Copy example config
cp configs/server.example.yaml configs/server.yaml

# Edit configuration as needed
nano configs/server.yaml

# Run tunnel server
./bin/tunnel-server -config configs/server.yaml
```

### 3. Connect Remote Servers

```bash
# Copy example config
cp configs/client.example.yaml configs/client.yaml

# Edit configuration (set server address, tunnels, etc.)
nano configs/client.yaml

# Run tunnel client
./bin/tunnel-client -config configs/client.yaml
```

## Development

### Testing

```bash
# Quick compilation and basic checks
make test-quick

# Run all tests (may have failures - development in progress)
make test

# Run specific test categories  
make test-server       # Server functionality
make test-client       # Client functionality
make test-integration  # Client-server integration

# Run with verbose output to see what's happening
make test-verbose
```

### Development Environment

```bash
# Setup development environment
make dev-setup

# Setup test environment with keys and configs
make setup-test-env

# Quick development cycle
make quick-test  # fmt + vet + basic tests
```

## Current Limitations

- Tests are still being stabilized
- Some edge cases in reconnection logic need work
- Performance optimization ongoing
- Documentation is incomplete
- No CLI management tool yet

## Project Status

🎯 **Current Stage: 3.1 - Tunnel Client Development**

### Completed
- [x] Project infrastructure and CI/CD
- [x] SSH tunnel server core functionality
- [x] Configuration system
- [x] Basic client implementation
- [x] Build and deployment scripts

### In Progress  
- [ ] Client test stabilization
- [ ] Auto-reconnection refinement
- [ ] Integration test improvements
- [ ] Error handling enhancement

### Next Steps
- [ ] CLI management tool
- [ ] Docker containerization  
- [ ] Production deployment guides
- [ ] Performance optimization

## Contributing

This project is in active development. Contributions are welcome, but please note:

1. Tests may be unstable - this is expected during development
2. Focus on core functionality first, polish comes later
3. Run `make test-quick` to check basic compilation
4. Full test suite with `make test` may have expected failures

## Performance Expectations

Current performance targets (under development):
- **Throughput**: ~50-100MB/s through tunnels
- **Connections**: 100+ concurrent clients  
- **Latency**: <10ms added latency for local connections
- **Memory**: <100MB per 1000 connections

*Note: These are development targets, actual performance may vary*

## Documentation

- [Development Plan](docs/DEVELOPMENT_PLAN.md) - Roadmap and progress
- [Testing Guide](docs/TESTING.md) - Testing documentation
- [Installation Guide](docs/INSTALLATION.md) (Coming soon)
- [Configuration Reference](docs/CONFIGURATION.md) (Coming soon)

## Security

- SSH key-based authentication
- No shell access - tunneling only
- Configurable port ranges
- Connection logging and monitoring

⚠️ **Security Note**: This is development software. Do not use in production without thorough security review.

## License

MIT License - see [LICENSE](LICENSE) for details.

## Acknowledgments

- Inspired by [Serveo](https://serveo.net/)
- Built with [Go](https://golang.org/) and [golang.org/x/crypto/ssh](https://pkg.go.dev/golang.org/x/crypto/ssh)
- Thanks to contributors and testers

---

**Note**: This README reflects the current development status. Information will be updated as features are completed and tested.
