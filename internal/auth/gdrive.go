package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

const (
	credentialsFile = "gdrive_credentials.json"
	tokenFile       = "gdrive_token.json"
)

func syncoDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, ".synco"), nil
}

func loadOAuthConfig() (*oauth2.Config, error) {
	dir, err := syncoDir()
	if err != nil {
		return nil, err
	}

	b, err := os.ReadFile(filepath.Join(dir, credentialsFile))
	if err != nil {
		return nil, fmt.Errorf("gdrive_credentials.json not found in ~/.synco: %w", err)
	}

	cfg, err := google.ConfigFromJSON(b, drive.DriveFileScope)
	if err != nil {
		return nil, fmt.Errorf("failed to parse credentials: %w", err)
	}

	return cfg, nil
}

func Authorize() error {
	cfg, err := loadOAuthConfig()
	if err != nil {
		return err
	}

	authURL := cfg.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Println("Visit the URL for the auth dialog:")
	fmt.Println()
	fmt.Println(authURL)
	fmt.Println()
	fmt.Print("Enter the code here: ")

	var code string
	if _, err := fmt.Scan(&code); err != nil {
		return fmt.Errorf("failed to read code: %w", err)
	}

	token, err := cfg.Exchange(context.Background(), code)
	if err != nil {
		return fmt.Errorf("failed to exchange token: %w", err)
	}

	return saveToken(token)
}

func saveToken(token *oauth2.Token) error {
	dir, err := syncoDir()
	if err != nil {
		return err
	}

	path := filepath.Join(dir, tokenFile)
	b, err := json.Marshal(token)
	if err != nil {
		return err
	}

	if err := os.WriteFile(path, b, 0600); err != nil {
		return fmt.Errorf("failed to save token: %w", err)
	}

	fmt.Printf("Token saved to %s\n", path)
	return nil
}

func loadToken() (*oauth2.Token, error) {
	dir, err := syncoDir()
	if err != nil {
		return nil, err
	}

	b, err := os.ReadFile(filepath.Join(dir, tokenFile))
	if err != nil {
		return nil, fmt.Errorf("gdrive auth needed. Please run 'synco auth gdrive' first: %w", err)
	}

	var token oauth2.Token
	if err := json.Unmarshal(b, &token); err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	return &token, nil
}

func NewDriveService(ctx context.Context) (*drive.Service, error) {
	cfg, err := loadOAuthConfig()
	if err != nil {
		return nil, err
	}

	token, err := loadToken()
	if err != nil {
		return nil, err
	}

	tokenSource := cfg.TokenSource(ctx, token)

	newToken, err := tokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to refresh token: %w", err)
	}

	if newToken.AccessToken != token.AccessToken {
		_ = saveToken(newToken)
	}

	svc, err := drive.NewService(ctx, option.WithTokenSource(tokenSource))
	if err != nil {
		return nil, fmt.Errorf("failed to create gdrive service: %w", err)
	}

	return svc, nil
}
