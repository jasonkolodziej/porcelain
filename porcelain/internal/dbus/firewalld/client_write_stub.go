//go:build !linux

package firewalld

import (
	"context"
	"fmt"
)

// Reload is unsupported on non-Linux hosts.
func (c *Client) Reload(_ context.Context) error {
	return fmt.Errorf("firewalld reload unsupported on this platform")
}

// SetServiceInZone is unsupported on non-Linux hosts.
func (c *Client) SetServiceInZone(_ context.Context, zone, service string, enabled bool) error {
	return fmt.Errorf("firewalld zone=%s service=%s enabled=%t unsupported on this platform", zone, service, enabled)
}
