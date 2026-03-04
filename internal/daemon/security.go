package daemon

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"synco/internal/util"
	"time"
)

const (
	certFile  = "daemon.crt"
	keyFile   = "daemon.key"
	tokenFile = "token"
)

type Credentials struct {
	TLSConfig *tls.Config
	Token     string
	CertPEM   []byte
}

func LoadOrCreateCredentials() (*Credentials, error) {
	dir, err := util.SyncoDir()
	if err != nil {
		return nil, err
	}

	certPath := filepath.Join(dir, certFile)
	keyPath := filepath.Join(dir, keyFile)
	tokenPath := filepath.Join(dir, tokenFile)

	if !isCertValid(certPath) {
		if err := generateCert(certPath, keyPath); err != nil {
			return nil, fmt.Errorf("failed to generate TLS cert: %w", err)
		}
	}

	if _, err := os.Stat(tokenPath); os.IsNotExist(err) {
		if err := generateToken(tokenPath); err != nil {
			return nil, fmt.Errorf("failed to generate token: %w", err)
		}
	}

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load TLS cert: %w", err)
	}

	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, err
	}

	tokenBytes, err := os.ReadFile(tokenPath)
	if err != nil {
		return nil, err
	}

	return &Credentials{
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS13,
		},
		Token:   string(tokenBytes),
		CertPEM: certPEM,
	}, nil
}

func isCertValid(certPath string) bool {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return false
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return false
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false
	}

	// 30일 이상 남아있어야 유효로 판단
	return time.Until(cert.NotAfter) > 30*24*time.Hour
}

func generateCert(certPath, keyPath string) error {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{Organization: []string{"synco"}},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().AddDate(1, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
		DNSNames:     []string{"localhost"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		return err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	if err := os.WriteFile(certPath, certPEM, 0600); err != nil {
		return err
	}

	privDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return err
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privDER})
	return os.WriteFile(keyPath, keyPEM, 0600)
}

func generateToken(tokenPath string) error {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return err
	}

	return os.WriteFile(tokenPath, []byte(hex.EncodeToString(b)), 0600)
}
