//go:build linux

package firewalld

import (
	"context"
	"fmt"
)

// Reload reloads firewalld runtime state.
func (c *Client) Reload(ctx context.Context) error {
	if c == nil || c.conn == nil {
		return fmt.Errorf("firewalld: nil connection")
	}
	root := c.conn.Object(dest, rootPath)
	call := root.CallWithContext(ctx, ifaceZone+".reload", 0)
	if call.Err != nil {
		return fmt.Errorf("firewalld reload: %w", call.Err)
	}
	return nil
}

// SetServiceInZone adds or removes a service in a zone.
func (c *Client) SetServiceInZone(ctx context.Context, zone, service string, enabled bool) error {
	if c == nil || c.conn == nil {
		return fmt.Errorf("firewalld: nil connection")
	}

	root := c.conn.Object(dest, rootPath)
	method := ifaceZone + ".addService"
	if !enabled {
		method = ifaceZone + ".removeService"
	}
	call := root.CallWithContext(ctx, method, 0, zone, service, int32(0))
	if call.Err != nil {
		return fmt.Errorf("firewalld %s service=%s zone=%s: %w", method, service, zone, call.Err)
	}
	return nil
}
