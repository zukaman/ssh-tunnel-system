# SSH Tunnel System

Self-hosted SSH tunnel system (Serveo alternative) for accessing servers behind NAT.

## Features

- **SSH Reverse Tunnels**: Access servers behind NAT via SSH
- **Auto-reconnection**: Clients automatically reconnect on connection loss
- **CLI Management**: Simple command-line interface for tunnel management
- **Secure**: TLS encryption and SSH key authentication
- **Lightweight**: Single binary deployment, minimal resource usage
- **Self-hosted**: Full control over your tunnel infrastructure

## Architecture

```
[Your Computer] → [Bridge Server] ← [Remote Servers behind NAT]
                       ↕
                 [CLI Management]
```

## Quick Start

### 1. Setup Bridge Server (VPS with public IP)

```bash
# Download and run tunnel-server
./tunnel-server -config server.yaml
```

### 2. Connect Remote Servers

```bash
# On each remote server behind NAT
./tunnel-client -config client.yaml
```

### 3. Manage Tunnels

```bash
# View connected servers
./tunnel-cli status

# Access remote server
ssh -p 2201 user@your-domain.com
```

## Project Status

🚧 **In Development** - This project is currently being developed.

### Completed
- [x] Project structure
- [x] Development plan

### In Progress
- [ ] SSH tunnel server implementation
- [ ] Client with auto-reconnection
- [ ] CLI management tool

## Documentation

- [Development Plan](docs/DEVELOPMENT_PLAN.md)
- [Installation Guide](docs/INSTALLATION.md) (Coming soon)
- [Configuration](docs/CONFIGURATION.md) (Coming soon)

## License

MIT License - see [LICENSE](LICENSE) for details.
