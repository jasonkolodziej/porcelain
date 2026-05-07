package policy

import (
	"context"
	"fmt"
	"strings"
)

// Input is the OPA-style authorization payload assembled from request context.
type Input struct {
	Subject   string
	Groups    []string
	Action    string
	Resource  string
	PeerCN    string
	PeerFP    string
	AuthToken string
}

// Decision is the result of policy evaluation.
type Decision struct {
	Allowed bool
	Reason  string
}

// Authorizer evaluates whether a request should be allowed.
type Authorizer interface {
	Authorize(ctx context.Context, input Input) (Decision, error)
}

// OPAStyleAuthorizer is an embedded policy engine with an OPA-compatible
// input model and explicit allowlist defaults.
type OPAStyleAuthorizer struct {
	adminGroup    string
	operatorGroup string
	authEnabled   bool
}

// NewOPAStyleAuthorizer returns a conservative policy evaluator intended to
// mirror future OPA/Rego rules while keeping this scaffold dependency-light.
func NewOPAStyleAuthorizer(adminGroup string, authEnabled bool) *OPAStyleAuthorizer {
	if strings.TrimSpace(adminGroup) == "" {
		adminGroup = "porcelain-admins"
	}
	return &OPAStyleAuthorizer{
		adminGroup:    adminGroup,
		operatorGroup: "porcelain-operators",
		authEnabled:   authEnabled,
	}
}

// Authorize applies a deny-by-default write policy and allow-by-default read
// policy for authenticated users.
func (a *OPAStyleAuthorizer) Authorize(_ context.Context, input Input) (Decision, error) {
	action := strings.ToLower(strings.TrimSpace(input.Action))
	if action == "" {
		return Decision{}, fmt.Errorf("policy action is required")
	}

	subject := strings.TrimSpace(input.Subject)
	if a.authEnabled && subject == "" {
		return Decision{Allowed: false, Reason: "missing authenticated subject"}, nil
	}

	// Read-only actions are broadly allowed for authenticated users.
	if strings.HasPrefix(action, "read.") {
		return Decision{Allowed: true, Reason: "read access"}, nil
	}

	// Write operations require elevated group membership.
	if strings.HasPrefix(action, "write.") {
		if hasGroup(input.Groups, a.adminGroup) || hasGroup(input.Groups, a.operatorGroup) {
			return Decision{Allowed: true, Reason: "group allowed"}, nil
		}
		return Decision{Allowed: false, Reason: "missing required group"}, nil
	}

	return Decision{Allowed: false, Reason: "unsupported policy action"}, nil
}

func hasGroup(groups []string, target string) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	if target == "" {
		return false
	}
	for _, g := range groups {
		if strings.ToLower(strings.TrimSpace(g)) == target {
			return true
		}
	}
	return false
}
