package policy

import (
	"context"
	"testing"
)

func TestOPAStyleAuthorizer_ReadAllowed(t *testing.T) {
	a := NewOPAStyleAuthorizer("porcelain-admins", true)

	decision, err := a.Authorize(context.Background(), Input{
		Subject:  "alice",
		Groups:   []string{"devs"},
		Action:   "read.network.devices",
		Resource: "network",
	})
	if err != nil {
		t.Fatalf("authorize read: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("expected read allowed, got denied: %s", decision.Reason)
	}
}

func TestOPAStyleAuthorizer_WriteDeniedWithoutGroup(t *testing.T) {
	a := NewOPAStyleAuthorizer("porcelain-admins", true)

	decision, err := a.Authorize(context.Background(), Input{
		Subject:  "alice",
		Groups:   []string{"devs"},
		Action:   "write.systemd.unit.restart",
		Resource: "sshd.service",
	})
	if err != nil {
		t.Fatalf("authorize write: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("expected write denied")
	}
}

func TestOPAStyleAuthorizer_WriteAllowedForAdmin(t *testing.T) {
	a := NewOPAStyleAuthorizer("porcelain-admins", true)

	decision, err := a.Authorize(context.Background(), Input{
		Subject:  "alice",
		Groups:   []string{"porcelain-admins"},
		Action:   "write.systemd.unit.restart",
		Resource: "sshd.service",
	})
	if err != nil {
		t.Fatalf("authorize write: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("expected write allowed, reason=%s", decision.Reason)
	}
}
