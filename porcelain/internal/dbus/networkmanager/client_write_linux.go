//go:build linux

package networkmanager

import (
	"context"
	"fmt"
)

// SetNetworkingEnabled toggles global networking via NetworkManager.
func (c *Client) SetNetworkingEnabled(ctx context.Context, enabled bool) error {
	if c == nil || c.conn == nil {
		return fmt.Errorf("networkmanager: nil connection")
	}
	root := c.conn.Object(dest, rootPath)
	call := root.CallWithContext(ctx, ifaceRoot+".Enable", 0, enabled)
	if call.Err != nil {
		return fmt.Errorf("networkmanager Enable(%t): %w", enabled, call.Err)
	}
	return nil
}
