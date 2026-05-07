//go:build !linux

package systemd

import (
	"context"
	"fmt"
)

// UnitAction is unsupported on non-Linux hosts.
func (c *Client) UnitAction(_ context.Context, unitName, action string) error {
	return fmt.Errorf("systemd %s %s unsupported on this platform", action, unitName)
}
