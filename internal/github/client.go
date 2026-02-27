package github

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/bradleyfalzon/ghinstallation/v2"
	gogithub "github.com/google/go-github/v60/github"
)

// NewGitHubClient creates a GitHub API client authenticated as a GitHub App
// installation. It uses ghinstallation for automatic JWT and installation
// token management.
//
// privateKey can be either:
//   - Raw PEM bytes (begins with "-----BEGIN")
//   - Base64-encoded PEM bytes
//
// If privateKey is nil or empty and privateKeyPath is provided, the key is
// read from that file path.
func NewGitHubClient(appID, installationID int64, privateKey []byte, privateKeyPath string) (*gogithub.Client, error) {
	key, err := resolvePrivateKey(privateKey, privateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("resolving private key: %w", err)
	}

	transport, err := ghinstallation.New(http.DefaultTransport, appID, installationID, key)
	if err != nil {
		return nil, fmt.Errorf("creating installation transport: %w", err)
	}

	client := gogithub.NewClient(&http.Client{Transport: transport})
	return client, nil
}

// resolvePrivateKey returns PEM-encoded private key bytes from either the
// provided raw/base64-encoded key or by reading from a file path.
func resolvePrivateKey(key []byte, keyPath string) ([]byte, error) {
	if len(key) > 0 {
		s := strings.TrimSpace(string(key))
		if strings.HasPrefix(s, "-----BEGIN") {
			return []byte(s), nil
		}
		// Try base64 decode
		decoded, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			// Try URL-safe base64
			decoded, err = base64.URLEncoding.DecodeString(s)
			if err != nil {
				return nil, fmt.Errorf("private key is neither PEM nor valid base64: %w", err)
			}
		}
		return decoded, nil
	}

	if keyPath != "" {
		data, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("reading private key file %s: %w", keyPath, err)
		}
		return data, nil
	}

	return nil, fmt.Errorf("no private key provided: set private_key or private_key_path")
}
