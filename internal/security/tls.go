package security

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// TLSConfig holds TLS configuration for secure communication
type TLSConfig struct {
	// Enabled controls whether TLS is enabled
	Enabled bool
	
	// CertFile is the path to the TLS certificate file
	CertFile string
	
	// KeyFile is the path to the TLS private key file
	KeyFile string
	
	// CAFile is the path to the CA certificate file (for mTLS)
	CAFile string
	
	// ClientAuth determines the client authentication policy
	// Options: "none", "request", "require", "verify" (mTLS)
	ClientAuth string
	
	// ServerName is the expected server name for certificate validation
	ServerName string
}

// LoadServerTLSConfig loads TLS configuration for gRPC server
func LoadServerTLSConfig(cfg TLSConfig) (*tls.Config, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	
	// Load server certificate and key
	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load server cert/key: %w", err)
	}
	
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13, // Require TLS 1.3 for security
	}
	
	// Configure client authentication for mTLS
	if cfg.CAFile != "" {
		caCert, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read CA cert: %w", err)
		}
		
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to add CA cert to pool")
		}
		
		tlsConfig.ClientCAs = caCertPool
		
		// Set client auth policy
		switch cfg.ClientAuth {
		case "require":
			tlsConfig.ClientAuth = tls.RequireAnyClientCert
		case "verify":
			tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
		default:
			tlsConfig.ClientAuth = tls.NoClientCert
		}
	}
	
	return tlsConfig, nil
}

// LoadClientTLSConfig loads TLS configuration for gRPC client
func LoadClientTLSConfig(cfg TLSConfig) (*tls.Config, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS13,
		ServerName: cfg.ServerName,
	}
	
	// Load client certificate for mTLS (optional)
	if cfg.CertFile != "" && cfg.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load client cert/key: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}
	
	// Load CA certificate for server verification
	if cfg.CAFile != "" {
		caCert, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read CA cert: %w", err)
		}
		
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to add CA cert to pool")
		}
		
		tlsConfig.RootCAs = caCertPool
	}
	
	return tlsConfig, nil
}

// ServerTLSOption creates gRPC server option for TLS
func ServerTLSOption(cfg TLSConfig) (grpc.ServerOption, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	
	tlsConfig, err := LoadServerTLSConfig(cfg)
	if err != nil {
		return nil, err
	}
	
	return grpc.Creds(credentials.NewTLS(tlsConfig)), nil
}

// ClientDialOption creates gRPC dial option for TLS
func ClientDialOption(cfg TLSConfig) (grpc.DialOption, error) {
	if !cfg.Enabled {
		return grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			InsecureSkipVerify: true,
		})), nil
	}
	
	tlsConfig, err := LoadClientTLSConfig(cfg)
	if err != nil {
		return nil, err
	}
	
	return grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)), nil
}
