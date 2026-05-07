package secrets

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	secretspkg "github.com/jasonkolodziej/porcelain/porcelain/pkg/secrets"
)

const systemdCredsBinary = "systemd-creds"

// SystemdCredsStore encrypts secrets with the host TPM through the systemd-creds CLI.
type SystemdCredsStore struct {
	storageDir string
}

// NewSystemdCredsStore validates the runtime dependency and prepares the credential directory.
func NewSystemdCredsStore(storageDir string) (*SystemdCredsStore, error) {
	if _, err := exec.LookPath(systemdCredsBinary); err != nil {
		return nil, fmt.Errorf("systemd-creds is required for this backend: %w", err)
	}
	if err := os.MkdirAll(storageDir, 0o700); err != nil {
		return nil, fmt.Errorf("create secrets directory: %w", err)
	}

	return &SystemdCredsStore{storageDir: storageDir}, nil
}

// Get decrypts the stored credential and returns the plaintext secret value.
func (s *SystemdCredsStore) Get(ctx context.Context, path string) (*secretspkg.SecretValue, error) {
	output, err := exec.CommandContext(ctx, systemdCredsBinary, "decrypt", s.filePath(path)).Output()
	if err != nil {
		return nil, fmt.Errorf("decrypt %q: %w", path, err)
	}

	return &secretspkg.SecretValue{Data: output}, nil
}

// Put encrypts the secret on write so it can remain on disk without exposing plaintext.
func (s *SystemdCredsStore) Put(ctx context.Context, path string, value *secretspkg.SecretValue) error {
	if err := os.MkdirAll(filepath.Dir(s.filePath(path)), 0o700); err != nil {
		return fmt.Errorf("create parent directory: %w", err)
	}

	command := exec.CommandContext(ctx, systemdCredsBinary, "encrypt", "-", s.filePath(path))
	command.Stdin = strings.NewReader(string(value.Data))
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("encrypt %q: %w: %s", path, err, strings.TrimSpace(string(output)))
	}

	return nil
}

// Delete removes the encrypted credential file.
func (s *SystemdCredsStore) Delete(_ context.Context, path string) error {
	if err := os.Remove(s.filePath(path)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete %q: %w", path, err)
	}

	return nil
}

// List enumerates encrypted credential files below the requested prefix.
func (s *SystemdCredsStore) List(_ context.Context, prefix string) ([]string, error) {
	matches := make([]string, 0)

	err := filepath.WalkDir(s.storageDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".cred") {
			return nil
		}

		relative, err := filepath.Rel(s.storageDir, path)
		if err != nil {
			return err
		}
		normalized := strings.TrimSuffix(filepath.ToSlash(relative), ".cred")
		if strings.HasPrefix(normalized, prefix) {
			matches = append(matches, normalized)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list credentials: %w", err)
	}

	sort.Strings(matches)
	return matches, nil
}

// Watch returns an explicit not-implemented error because systemd-creds has no native watch API.
func (s *SystemdCredsStore) Watch(_ context.Context, _ string) (<-chan *secretspkg.SecretValue, error) {
	return nil, secretspkg.ErrNotImplemented
}

// filePath maps a logical secret path to the encrypted credential file on disk.
func (s *SystemdCredsStore) filePath(path string) string {
	return filepath.Join(s.storageDir, filepath.FromSlash(path)+".cred")
}
