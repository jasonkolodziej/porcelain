// Package firewalld wraps org.fedoraproject.FirewallD1. The current surface
// is read-only and small — listing zones, the default zone, and active
// services — pending the rule-editing flows in Phase 3.
package firewalld

import (
	"context"
	"fmt"

	godbus "github.com/godbus/dbus/v5"
)

const (
	dest         = "org.fedoraproject.FirewallD1"
	rootPath     = "/org/fedoraproject/FirewallD1"
	ifaceConfig  = "org.fedoraproject.FirewallD1.config"
	ifaceZoneCfg = "org.fedoraproject.FirewallD1.config.zone"
	ifaceZone    = "org.fedoraproject.FirewallD1.zone"
)

// ZoneSummary lists active zones, the default zone, and the services
// activated in the default zone.
type ZoneSummary struct {
	Default          string
	Zones            []string
	ServicesByZone   map[string][]string
	InterfacesByZone map[string][]string
}

// Client wraps a godbus connection scoped to firewalld.
type Client struct{ conn *godbus.Conn }

// New builds a Client around an already-connected bus.
func New(conn *godbus.Conn) *Client { return &Client{conn: conn} }

// Summary returns a flattened view of the current firewalld state.
func (c *Client) Summary(ctx context.Context) (ZoneSummary, error) {
	if c == nil || c.conn == nil {
		return ZoneSummary{}, fmt.Errorf("firewalld: nil connection")
	}

	root := c.conn.Object(dest, rootPath)

	out := ZoneSummary{
		ServicesByZone:   map[string][]string{},
		InterfacesByZone: map[string][]string{},
	}

	def := root.CallWithContext(ctx, ifaceZone+".getDefaultZone", 0)
	if def.Err != nil {
		return out, fmt.Errorf("firewalld getDefaultZone: %w", def.Err)
	}
	if err := def.Store(&out.Default); err != nil {
		return out, err
	}

	zones := root.CallWithContext(ctx, ifaceZone+".getZones", 0)
	if zones.Err != nil {
		return out, fmt.Errorf("firewalld getZones: %w", zones.Err)
	}
	if err := zones.Store(&out.Zones); err != nil {
		return out, err
	}

	for _, z := range out.Zones {
		svc := root.CallWithContext(ctx, ifaceZone+".getServices", 0, z)
		if svc.Err == nil {
			var services []string
			if err := svc.Store(&services); err == nil {
				out.ServicesByZone[z] = services
			}
		}
		ifs := root.CallWithContext(ctx, ifaceZone+".getInterfaces", 0, z)
		if ifs.Err == nil {
			var ifaces []string
			if err := ifs.Store(&ifaces); err == nil {
				out.InterfacesByZone[z] = ifaces
			}
		}
	}
	return out, nil
}
