//go:build !linux

package networkmanager

import (
	"context"
	"fmt"
)

// SetNetworkingEnabled is unsupported on non-Linux hosts.
func (c *Client) SetNetworkingEnabled(_ context.Context, enabled bool) error {
	return fmt.Errorf("networkmanager enable=%t unsupported on this platform", enabled)
}
