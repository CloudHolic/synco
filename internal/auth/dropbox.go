package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/oauth2"
)

const (
	dropboxCredFile  = "dropbox_credentials.json"
	dropboxTokenFile = "dropbox_token.json"
)

type dropboxCredentials struct {
	AppKey    string `json:"app_key"`
	AppSecret string `json:"app_secret"`
}

var dropboxEndpoint = oauth2.Endpoint{
	AuthURL:  "https://www.dropbox.com/oauth2/authorize",
	TokenURL: "https://api.dropboxapi.com/oauth2/token",
}

func loadDropboxConfig() (*oauth2.Config, error) {
	dir, err := syncoDir()
	if err != nil {
		return nil, err
	}

	b, err := os.ReadFile(filepath.Join(dir, dropboxCredFile))
	if err != nil {
		return nil, fmt.Errorf("dropbox_credentials.json not found in ~/.synco: %w", err)
	}

	var creds dropboxCredentials
	if err := json.Unmarshal(b, &creds); err != nil {
		return nil, fmt.Errorf("failed to parse dropbox credentials: %w", err)
	}

	return &oauth2.Config{
		ClientID:     creds.AppKey,
		ClientSecret: creds.AppSecret,
		Endpoint:     dropboxEndpoint,
		RedirectURL:  "http://localhost:9999/callback",
		Scopes:       []string{"files.content.read", "files.content.write"},
	}, nil
}

func AuthorizeDropbox() error {
	cfg, err := loadDropboxConfig()
	if err != nil {
		return err
	}

	authURL := cfg.AuthCodeURL("state-token",
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("token_access_type", "offline"))

	fmt.Println("Visit the URL for the auth dialog:")
	fmt.Println()
	fmt.Println(authURL)
	fmt.Println()

	codeCh := make(chan string, 1)
	srv := &http.Server{Addr: ":9999"}

	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		codeCh <- code
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprintln(w, "<h2>Authentication complete! Now you can close this window and return to the terminal.</h2>")
	})

	go func() { _ = srv.ListenAndServe() }()

	fmt.Println("Authentication will complete after you log on via browser...")

	select {
	case code := <-codeCh:
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)

		token, err := cfg.Exchange(context.Background(), code)
		if err != nil {
			return fmt.Errorf("failed to exchange token: %w", err)
		}

		return saveDropboxToken(token)

	case <-time.After(2 * time.Minute):
		_ = srv.Shutdown(context.Background())
		return fmt.Errorf("authorization timed out")
	}
}

func saveDropboxToken(token *oauth2.Token) error {
	dir, err := syncoDir()
	if err != nil {
		return err
	}

	b, err := json.Marshal(token)
	if err != nil {
		return err
	}

	path := filepath.Join(dir, dropboxTokenFile)
	if err := os.WriteFile(path, b, 0600); err != nil {
		return fmt.Errorf("failed to save token: %w", err)
	}

	fmt.Printf("Dropbox token saved to %s\n", path)
	return nil
}

func loadDropboxToken() (*oauth2.Token, error) {
	dir, err := syncoDir()
	if err != nil {
		return nil, err
	}

	b, err := os.ReadFile(filepath.Join(dir, dropboxTokenFile))
	if err != nil {
		return nil, fmt.Errorf("dropbox auth needed. Please run 'synco auth dropbox' first: %w", err)
	}

	var token oauth2.Token
	if err := json.Unmarshal(b, &token); err != nil {
		return nil, fmt.Errorf("failed to parse dropbox token: %w", err)
	}

	return &token, nil
}

func NewDropboxToken() (*oauth2.Token, error) {
	cfg, err := loadDropboxConfig()
	if err != nil {
		return nil, err
	}

	token, err := loadDropboxToken()
	if err != nil {
		return nil, err
	}

	tokenSource := cfg.TokenSource(context.Background(), token)
	newToken, err := tokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to refresh dropbox token: %w", err)
	}

	if newToken.AccessToken != token.AccessToken {
		_ = saveDropboxToken(newToken)
	}

	return token, nil
}
