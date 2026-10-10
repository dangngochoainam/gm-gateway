package configs

import (
	"fmt"
	"time"

	"github.com/dangngochoainam/gopkg/confighelper"
)

// yaml format
var defaultConfig = []byte(`
env: dev
http_port: 50051
grpc_services:
  product:
    host: localhost
    port: 50060
server:
  read_header_timeout: 5s
  read_timeout: 15s
  write_timeout: 30s
  idle_timeout: 60s
  request_timeout: 10s
  shutdown_drain_delay: 5s
  shutdown_timeout: 10s
  max_request_body_bytes: 1048576
  max_header_bytes: 8192
allowed_origins: "http://localhost:5173,http://172.93.163.153"

`)

type (
	Config struct {
		Env            string             `mapstructure:"env"`
		HttpPort       string             `mapstructure:"http_port"`
		GrpcServices   GrpcServicesConfig `mapstructure:"grpc_services"`
		Server         ServerConfig       `mapstructure:"server"`
		AllowedOrigins []string           `mapstructure:"allowed_origins"`
	}

	// GrpcServicesConfig holds the address of every downstream domain service
	// the gateway forwards to. Add one field per service.
	GrpcServicesConfig struct {
		Product GrpcServiceConfig `mapstructure:"product"`
	}

	GrpcServiceConfig struct {
		Host string `mapstructure:"host"`
		Port uint32 `mapstructure:"port"`
	}

	// ServerConfig holds the HTTP server limits and the deadline applied to
	// every request. No call may run without a deadline: a hung downstream
	// service would otherwise hold gateway connections until the pod dies.
	//
	// RequestTimeout is the deadline propagated to the downstream gRPC call, so
	// it must stay slightly ABOVE the downstream timeout. The gateway then
	// receives the downstream error instead of cutting the call off itself.
	// It must also stay below WriteTimeout, or the error response cannot be
	// written.
	ServerConfig struct {
		ReadHeaderTimeout   time.Duration `mapstructure:"read_header_timeout"`
		ReadTimeout         time.Duration `mapstructure:"read_timeout"`
		WriteTimeout        time.Duration `mapstructure:"write_timeout"`
		IdleTimeout         time.Duration `mapstructure:"idle_timeout"`
		RequestTimeout      time.Duration `mapstructure:"request_timeout"`
		ShutdownDrainDelay  time.Duration `mapstructure:"shutdown_drain_delay"`
		ShutdownTimeout     time.Duration `mapstructure:"shutdown_timeout"`
		MaxRequestBodyBytes int64         `mapstructure:"max_request_body_bytes"`
		MaxHeaderBytes      int           `mapstructure:"max_header_bytes"`
	}
)

func LoadConfig() (*Config, error) {
	configMap := &Config{}

	err := confighelper.Load(configMap, defaultConfig)
	if err != nil {
		fmt.Printf("Error while load config, err: %v", err)
		return nil, err
	}

	return configMap, nil
}
