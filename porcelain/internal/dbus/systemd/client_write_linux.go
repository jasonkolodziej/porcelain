//go:build linux

package systemd

import (
	"context"
	"fmt"
	"strings"
)

// UnitAction invokes StartUnit/StopUnit/RestartUnit on systemd.
func (c *Client) UnitAction(ctx context.Context, unitName, action string) error {
	if c == nil || c.conn == nil {
		return fmt.Errorf("systemd: nil connection")
	}

	unitName = strings.TrimSpace(unitName)
	action = strings.ToLower(strings.TrimSpace(action))
	if unitName == "" {
		return fmt.Errorf("systemd: unit name is required")
	}

	var method string
	switch action {
	case "start":
		method = mgr + ".StartUnit"
	case "stop":
		method = mgr + ".StopUnit"
	case "restart":
		method = mgr + ".RestartUnit"
	default:
		return fmt.Errorf("systemd: unsupported action %q", action)
	}

	obj := c.conn.Object(dest, path)
	call := obj.CallWithContext(ctx, method, 0, unitName, "replace")
	if call.Err != nil {
		return fmt.Errorf("systemd %s %s: %w", action, unitName, call.Err)
	}
	return nil
}
