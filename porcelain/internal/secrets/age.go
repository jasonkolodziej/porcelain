package secrets

import (
	"context"

	secretspkg "github.com/jasonkolodziej/porcelain/porcelain/pkg/secrets"
)

// AgeStore reserves the extension point for file-based age encryption in development and air-gapped installs.
type AgeStore struct{}

// NewAgeStore returns a scaffolded backend placeholder until age-based persistence is implemented.
func NewAgeStore(_ string, _ string, _ string) (*AgeStore, error) {
	return &AgeStore{}, nil
}

// Get reports that the age backend still needs its concrete implementation.
func (a *AgeStore) Get(_ context.Context, _ string) (*secretspkg.SecretValue, error) {
	return nil, secretspkg.ErrNotImplemented
}

// Put reports that the age backend still needs its concrete implementation.
func (a *AgeStore) Put(_ context.Context, _ string, _ *secretspkg.SecretValue) error {
	return secretspkg.ErrNotImplemented
}

// Delete reports that the age backend still needs its concrete implementation.
func (a *AgeStore) Delete(_ context.Context, _ string) error {
	return secretspkg.ErrNotImplemented
}

// List reports that the age backend still needs its concrete implementation.
func (a *AgeStore) List(_ context.Context, _ string) ([]string, error) {
	return nil, secretspkg.ErrNotImplemented
}

// Watch reports that the age backend still needs its concrete implementation.
func (a *AgeStore) Watch(_ context.Context, _ string) (<-chan *secretspkg.SecretValue, error) {
	return nil, secretspkg.ErrNotImplemented
}
