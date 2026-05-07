package dbus

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"

	godbus "github.com/godbus/dbus/v5"
)

// ErrBusUnavailable is returned by Dial when the configured bus cannot be
// reached. Callers may treat this as a soft failure and fall back to a
// platform-specific developer mock.
var ErrBusUnavailable = errors.New("d-bus bus unavailable")

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
	// Optional indicates that a missing system bus should be logged as a warning
	// rather than failing daemon startup. This is appropriate for local dev,
	// container-based UI smoke tests, and unit tests where host integration is
	// not exercised.
	Optional bool
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
			if m.config.Optional {
				return nil
			}
			return fmt.Errorf("system bus socket is not available: %w", err)
		}
	}

	return nil
}

// Config returns the resolved connection settings so modules can derive client-specific behavior.
func (m *ConnectionManager) Config() ConnectionConfig {
	return m.config
}

// Dial opens a real godbus connection on the configured bus. On non-Linux
// hosts and on Linux hosts without a system bus socket, it returns
// ErrBusUnavailable so module code can substitute a developer fake.
func (m *ConnectionManager) Dial(_ context.Context) (*godbus.Conn, error) {
	if runtime.GOOS != "linux" {
		return nil, fmt.Errorf("%w: %s is not supported", ErrBusUnavailable, runtime.GOOS)
	}

	if m.config.Bus == SystemBus && m.config.Address == "" {
		if _, err := os.Stat("/run/dbus/system_bus_socket"); err != nil {
			return nil, fmt.Errorf("%w: system bus socket missing: %s", ErrBusUnavailable, err)
		}
	}

	var (
		conn *godbus.Conn
		err  error
	)
	switch {
	case m.config.Address != "":
		conn, err = godbus.Dial(m.config.Address)
		if err == nil {
			if authErr := conn.Auth(nil); authErr != nil {
				_ = conn.Close()
				return nil, fmt.Errorf("%w: auth: %s", ErrBusUnavailable, authErr)
			}
			if helloErr := conn.Hello(); helloErr != nil {
				_ = conn.Close()
				return nil, fmt.Errorf("%w: hello: %s", ErrBusUnavailable, helloErr)
			}
		}
	case m.config.Bus == SessionBus:
		conn, err = godbus.SessionBus()
	default:
		conn, err = godbus.SystemBus()
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrBusUnavailable, err)
	}
	return conn, nil
}
