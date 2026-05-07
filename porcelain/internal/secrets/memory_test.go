package secrets_test

import (
	"context"
	"testing"

	"github.com/jasonkolodziej/porcelain/porcelain/internal/secrets"
	secretspkg "github.com/jasonkolodziej/porcelain/porcelain/pkg/secrets"
)

// TestMemoryStorePutGet validates the development backend round-trips secrets
// without leaking the same buffer to multiple callers.
func TestMemoryStorePutGet(t *testing.T) {
	store := secrets.NewMemoryStore()
	ctx := context.Background()

	if err := store.Put(ctx, "auth/dex/client-secret", &secretspkg.SecretValue{Data: []byte("hunter2")}); err != nil {
		t.Fatalf("put: %v", err)
	}

	got, err := store.Get(ctx, "auth/dex/client-secret")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got.Data) != "hunter2" {
		t.Fatalf("data: %q", got.Data)
	}

	got.Data[0] = 'X'
	again, err := store.Get(ctx, "auth/dex/client-secret")
	if err != nil {
		t.Fatalf("get again: %v", err)
	}
	if string(again.Data) != "hunter2" {
		t.Fatalf("memory store leaked buffer: %q", again.Data)
	}
}

// TestMemoryStoreListPrefix exercises the prefix filter the future watch and
// rotation flows depend on.
func TestMemoryStoreListPrefix(t *testing.T) {
	store := secrets.NewMemoryStore()
	ctx := context.Background()

	for _, path := range []string{"a/x", "a/y", "b/z"} {
		if err := store.Put(ctx, path, &secretspkg.SecretValue{Data: []byte("v")}); err != nil {
			t.Fatalf("put %s: %v", path, err)
		}
	}

	paths, err := store.List(ctx, "a/")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(paths) != 2 || paths[0] != "a/x" || paths[1] != "a/y" {
		t.Fatalf("paths: %v", paths)
	}
}
