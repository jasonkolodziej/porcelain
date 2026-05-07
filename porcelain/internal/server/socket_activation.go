package server

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"

	"github.com/jasonkolodziej/porcelain/porcelain/internal/config"
)

const firstSystemdFD = 3

// Listen prefers a systemd-activated socket and falls back to a normal TCP listener for development.
func Listen(_ context.Context, cfg config.ServerConfig) (net.Listener, error) {
	if cfg.EnableSocketActivation {
		listener, err := activatedListener()
		if err == nil {
			return listener, nil
		}
	}

	return net.Listen("tcp", cfg.Address)
}

// activatedListener returns the first inherited listener when systemd socket activation is present.
func activatedListener() (net.Listener, error) {
	pidValue := os.Getenv("LISTEN_PID")
	fdsValue := os.Getenv("LISTEN_FDS")
	if pidValue == "" || fdsValue == "" {
		return nil, fmt.Errorf("socket activation environment is not present")
	}

	pid, err := strconv.Atoi(pidValue)
	if err != nil || pid != os.Getpid() {
		return nil, fmt.Errorf("socket activation PID does not match current process")
	}

	fds, err := strconv.Atoi(fdsValue)
	if err != nil || fds < 1 {
		return nil, fmt.Errorf("socket activation does not expose any listeners")
	}

	file := os.NewFile(uintptr(firstSystemdFD), "porcelain-listener")
	defer file.Close()

	listener, err := net.FileListener(file)
	if err != nil {
		return nil, fmt.Errorf("convert systemd file descriptor to listener: %w", err)
	}

	return listener, nil
}
