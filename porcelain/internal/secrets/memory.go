package secrets

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	secretspkg "github.com/jasonkolodziej/porcelain/porcelain/pkg/secrets"
)

// MemoryStore keeps secrets in memory so the scaffold is runnable on any developer workstation.
type MemoryStore struct {
	mu       sync.RWMutex
	values   map[string]*secretspkg.SecretValue
	watchers map[string][]chan *secretspkg.SecretValue
}

// NewMemoryStore creates the default development secret backend.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		values:   make(map[string]*secretspkg.SecretValue),
		watchers: make(map[string][]chan *secretspkg.SecretValue),
	}
}

// Get returns a copied secret value so callers cannot mutate the in-memory store by accident.
func (m *MemoryStore) Get(_ context.Context, path string) (*secretspkg.SecretValue, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	value, ok := m.values[path]
	if !ok {
		return nil, fmt.Errorf("secret %q: not found", path)
	}

	return cloneSecretValue(value), nil
}

// Put writes the secret and fan-outs a copy to any registered watchers.
func (m *MemoryStore) Put(_ context.Context, path string, value *secretspkg.SecretValue) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cloned := cloneSecretValue(value)
	if cloned.CreatedAt.IsZero() {
		cloned.CreatedAt = time.Now().UTC()
	}
	m.values[path] = cloned

	for _, watcher := range m.watchers[path] {
		select {
		case watcher <- cloneSecretValue(cloned):
		default:
		}
	}

	return nil
}

// Delete removes a secret from the in-memory index.
func (m *MemoryStore) Delete(_ context.Context, path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.values, path)
	return nil
}

// List returns every matching secret path below the requested prefix.
func (m *MemoryStore) List(_ context.Context, prefix string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	items := make([]string, 0)
	for path := range m.values {
		if strings.HasPrefix(path, prefix) {
			items = append(items, path)
		}
	}
	sort.Strings(items)

	return items, nil
}

// Watch registers an in-memory channel for future secret writes.
func (m *MemoryStore) Watch(ctx context.Context, path string) (<-chan *secretspkg.SecretValue, error) {
	updates := make(chan *secretspkg.SecretValue, 1)

	m.mu.Lock()
	m.watchers[path] = append(m.watchers[path], updates)
	m.mu.Unlock()

	go func() {
		<-ctx.Done()
		close(updates)
	}()

	return updates, nil
}

// cloneSecretValue duplicates the secret payload so callers and watchers receive isolated values.
func cloneSecretValue(in *secretspkg.SecretValue) *secretspkg.SecretValue {
	if in == nil {
		return &secretspkg.SecretValue{}
	}

	out := *in
	out.Data = append([]byte(nil), in.Data...)
	if in.Metadata != nil {
		out.Metadata = make(map[string]string, len(in.Metadata))
		for key, value := range in.Metadata {
			out.Metadata[key] = value
		}
	}

	return &out
}
