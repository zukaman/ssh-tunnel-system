package config

import (
	"time"

	"github.com/spf13/viper"
)

// ServerConfig contains all server configuration
type ServerConfig struct {
	Server     ServerSettings    `mapstructure:"server"`
	Auth       AuthConfig        `mapstructure:"auth"`
	Logging    LoggingConfig     `mapstructure:"logging"`
	Monitoring MonitoringConfig  `mapstructure:"monitoring"`
	Database   DatabaseConfig    `mapstructure:"database"`
}

// ServerSettings contains server-specific settings
type ServerSettings struct {
	SSHHost      string    `mapstructure:"ssh_host"`
	SSHPort      int       `mapstructure:"ssh_port"`
	GRPCHost     string    `mapstructure:"grpc_host"`
	GRPCPort     int       `mapstructure:"grpc_port"`
	Domain       string    `mapstructure:"domain"`
	HostKeyPath  string    `mapstructure:"host_key_path"`
	PortRange    PortRange `mapstructure:"port_range"`
}

// PortRange defines range of ports for client tunnels
type PortRange struct {
	Start int `mapstructure:"start"`
	End   int `mapstructure:"end"`
}

// ClientConfig contains all client configuration
type ClientConfig struct {
	Server     ClientServerConfig `mapstructure:"server"`
	Tunnels    []TunnelConfig     `mapstructure:"tunnels"`
	Connection ConnectionConfig   `mapstructure:"connection"`
	Logging    LoggingConfig      `mapstructure:"logging"`
	Health     HealthConfig       `mapstructure:"health"`
	Service    ServiceConfig      `mapstructure:"service"`
	ClientID   string             `mapstructure:"client_id"`
}

// ClientServerConfig contains server connection settings for client
type ClientServerConfig struct {
	Host           string `mapstructure:"host"`
	Port           int    `mapstructure:"port"`
	PrivateKeyPath string `mapstructure:"private_key_path"`
}

// TunnelConfig defines a tunnel configuration
type TunnelConfig struct {
	Name      string `mapstructure:"name"`
	LocalHost string `mapstructure:"local_host"`
	LocalPort int    `mapstructure:"local_port"`
	Protocol  string `mapstructure:"protocol"`
}

// ConnectionConfig contains connection-related settings
type ConnectionConfig struct {
	RetryInterval     time.Duration `mapstructure:"retry_interval"`
	MaxRetries        int           `mapstructure:"max_retries"`
	KeepaliveInterval time.Duration `mapstructure:"keepalive_interval"`
	KeepaliveTimeout  time.Duration `mapstructure:"keepalive_timeout"`
	ConnectTimeout    time.Duration `mapstructure:"connect_timeout"`
}

// AuthConfig contains authentication settings
type AuthConfig struct {
	AuthorizedKeysPath string `mapstructure:"authorized_keys_path"`
	AllowRegistration  bool   `mapstructure:"allow_registration"`
	MaxClientsPerKey   int    `mapstructure:"max_clients_per_key"`
}

// LoggingConfig contains logging settings
type LoggingConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
	File   string `mapstructure:"file"`
}

// MonitoringConfig contains monitoring settings
type MonitoringConfig struct {
	PrometheusEnabled     bool          `mapstructure:"prometheus_enabled"`
	PrometheusPort        int           `mapstructure:"prometheus_port"`
	HealthCheckInterval   time.Duration `mapstructure:"health_check_interval"`
	ClientTimeout         time.Duration `mapstructure:"client_timeout"`
}

// DatabaseConfig contains database settings
type DatabaseConfig struct {
	Type     string `mapstructure:"type"`
	Path     string `mapstructure:"path"`
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	Database string `mapstructure:"database"`
}

// HealthConfig contains health monitoring settings
type HealthConfig struct {
	Enabled         bool          `mapstructure:"enabled"`
	Port            int           `mapstructure:"port"`
	ReportMetrics   bool          `mapstructure:"report_metrics"`
	MetricsInterval time.Duration `mapstructure:"metrics_interval"`
}

// ServiceConfig contains service settings
type ServiceConfig struct {
	User             string `mapstructure:"user"`
	WorkingDirectory string `mapstructure:"working_directory"`
	RestartPolicy    string `mapstructure:"restart_policy"`
}

// LoadServerConfig loads server configuration from file
func LoadServerConfig(configPath string) (*ServerConfig, error) {
	viper.SetConfigFile(configPath)
	viper.SetConfigType("yaml")

	// Set defaults
	viper.SetDefault("server.ssh_host", "0.0.0.0")
	viper.SetDefault("server.ssh_port", 2222)
	viper.SetDefault("server.grpc_host", "0.0.0.0")
	viper.SetDefault("server.grpc_port", 8080)
	viper.SetDefault("server.port_range.start", 2200)
	viper.SetDefault("server.port_range.end", 2300)
	viper.SetDefault("auth.allow_registration", true)
	viper.SetDefault("auth.max_clients_per_key", 5)
	viper.SetDefault("logging.level", "info")
	viper.SetDefault("logging.format", "text")
	viper.SetDefault("monitoring.prometheus_enabled", true)
	viper.SetDefault("monitoring.prometheus_port", 9090)
	viper.SetDefault("monitoring.health_check_interval", "30s")
	viper.SetDefault("monitoring.client_timeout", "5m")
	viper.SetDefault("database.type", "sqlite")
	viper.SetDefault("database.path", "./clients.db")

	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}

	var config ServerConfig
	if err := viper.Unmarshal(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

// LoadClientConfig loads client configuration from file
func LoadClientConfig(configPath string) (*ClientConfig, error) {
	viper.SetConfigFile(configPath)
	viper.SetConfigType("yaml")

	// Set defaults
	viper.SetDefault("server.port", 2222)
	viper.SetDefault("connection.retry_interval", "10s")
	viper.SetDefault("connection.max_retries", 0)
	viper.SetDefault("connection.keepalive_interval", "30s")
	viper.SetDefault("connection.keepalive_timeout", "10s")
	viper.SetDefault("connection.connect_timeout", "15s")
	viper.SetDefault("logging.level", "info")
	viper.SetDefault("logging.format", "text")
	viper.SetDefault("health.enabled", true)
	viper.SetDefault("health.port", 8081)
	viper.SetDefault("health.report_metrics", true)
	viper.SetDefault("health.metrics_interval", "60s")
	viper.SetDefault("service.restart_policy", "always")

	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}

	var config ClientConfig
	if err := viper.Unmarshal(&config); err != nil {
		return nil, err
	}

	return &config, nil
}
