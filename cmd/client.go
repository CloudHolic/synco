package cmd

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"synco/internal/util"
)

var (
	apiClient *http.Client
	apiToken  string
)

func initAPIClient() error {
	dir, err := util.SyncoDir()
	if err != nil {
		return err
	}

	if b, err := os.ReadFile(filepath.Join(dir, "token")); err != nil {
		apiToken = strings.TrimSpace(string(b))
	}

	certPEM, err := os.ReadFile(filepath.Join(dir, "daemon.crt"))
	if err != nil {
		apiClient = http.DefaultClient
		return nil
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(certPEM) {
		return fmt.Errorf("failed to parse daemon certificate")
	}

	apiClient = &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: pool,
			},
		},
	}
	return nil
}

func daemonURL(path string) string {
	return fmt.Sprintf("https://127.0.0.1:%d%s", cfg.DaemonPort, path)
}

func newRequest(method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, daemonURL(path), nil)
	if err != nil {
		return nil, err
	}

	if apiToken != "" {
		req.Header.Set("Authorization", "Bearer "+apiToken)
	}

	return req, nil
}

func apiGet(path string) (*http.Response, error) {
	req, err := newRequest(http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	return apiClient.Do(req)
}

func apiPost(path, contentType string, body io.Reader) (*http.Response, error) {
	req, err := newRequest(http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	return apiClient.Do(req)
}

func apiDelete(path string) (*http.Response, error) {
	req, err := newRequest(http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}

	return apiClient.Do(req)
}
