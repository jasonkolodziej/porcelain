package dbus

import (
	"context"
	"fmt"
	"os"
)

// BusType identifies the runtime bus used by a client connection.
type BusType string

const (
	// SystemBus is the default bus for host-level services such as systemd and NetworkManager.
	SystemBus BusType = "system"
	// SessionBus is reserved for future user-scoped integrations.
	SessionBus BusType = "session"
)

// ConnectionConfig declares how the daemon should discover and authorize its D-Bus access.
type ConnectionConfig struct {
	Address                string
	Bus                    BusType
	EnforcePeerCredentials bool
}

// ConnectionManager owns the D-Bus bootstrap boundary for future host integration clients.
type ConnectionManager struct {
	config ConnectionConfig
}

// NewConnectionManager creates the D-Bus integration boundary used by feature modules.
func NewConnectionManager(cfg ConnectionConfig) *ConnectionManager {
	if cfg.Bus == "" {
		cfg.Bus = SystemBus
	}

	return &ConnectionManager{config: cfg}
}

// Connect validates the runtime expectations before a concrete D-Bus client is attached.
func (m *ConnectionManager) Connect(_ context.Context) error {
	if m.config.Bus == SystemBus && m.config.Address == "" {
		if _, err := os.Stat("/run/dbus/system_bus_socket"); err != nil {
			return fmt.Errorf("system bus socket is not available: %w", err)
		}
	}

	return nil
}

// Config returns the resolved connection settings so modules can derive client-specific behavior.
func (m *ConnectionManager) Config() ConnectionConfig {
	return m.config
}
