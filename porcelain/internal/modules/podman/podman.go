// Package podman exposes a sidebar-visible Module summarising local Podman
// availability and basic runtime reachability.
package podman

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/jasonkolodziej/porcelain/porcelain/internal/modules"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/viewdata"
)

// Module implements modules.Module for the Containers page.
type Module struct {
	mode string
	note string
}

// New returns a Module that probes Podman availability.
func New(ctx context.Context) *Module {
	if _, err := exec.LookPath("podman"); err != nil {
		return &Module{mode: "fake", note: "podman not installed"}
	}

	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, "podman", "system", "info", "--format", "json")
	if err := cmd.Run(); err != nil {
		if errors.Is(probeCtx.Err(), context.DeadlineExceeded) {
			return &Module{mode: "degraded", note: "podman probe timed out"}
		}
		return &Module{mode: "degraded", note: "podman installed but daemon unavailable"}
	}

	return &Module{mode: "podman"}
}

// ID implements modules.Module.
func (m *Module) ID() string { return "podman" }

// Name implements modules.Module.
func (m *Module) Name() string { return "Containers" }

// Status implements modules.Module.
func (m *Module) Status(_ context.Context) modules.Status {
	switch m.mode {
	case "podman":
		return modules.Status{Health: modules.HealthOK, Detail: "podman runtime reachable"}
	case "degraded":
		detail := m.note
		if detail == "" {
			detail = "podman partially available"
		}
		return modules.Status{Health: modules.HealthDegraded, Detail: detail}
	case "fake":
		detail := m.note
		if detail == "" {
			detail = "podman not installed"
		}
		return modules.Status{Health: modules.HealthDegraded, Detail: detail}
	default:
		return modules.Status{Health: modules.HealthUnavailable}
	}
}

// Close implements modules.Module.
func (m *Module) Close() error { return nil }

// Snapshot returns a podman runtime overview suitable for the containers page.
func (m *Module) Snapshot(ctx context.Context) (viewdata.PodmanPageData, error) {
	data := viewdata.PodmanPageData{
		Heading: "Containers",
		Blurb:   "Podman runtime, images, and container lifecycle",
	}

	if m == nil || m.mode == "fake" {
		if m != nil && m.note != "" {
			data.Detail = m.note
		} else {
			data.Detail = "podman not installed"
		}
		return data, nil
	}

	if m.mode == "degraded" {
		data.Detail = m.note
		return data, nil
	}

	containers, err := listContainers(ctx)
	if err != nil {
		return data, fmt.Errorf("list containers: %w", err)
	}
	images, err := listImages(ctx)
	if err != nil {
		return data, fmt.Errorf("list images: %w", err)
	}

	data.Containers = containers
	data.Images = images
	data.Detail = fmt.Sprintf("%d containers, %d images", len(containers), len(images))
	return data, nil
}

// ContainerAction performs a safe lifecycle action on one container.
func (m *Module) ContainerAction(ctx context.Context, containerID, action string) error {
	if m == nil || m.mode == "fake" {
		return fmt.Errorf("podman not installed")
	}
	action = strings.ToLower(strings.TrimSpace(action))
	switch action {
	case "start", "stop", "restart":
	default:
		return fmt.Errorf("unsupported podman action %q", action)
	}
	containerID = strings.TrimSpace(containerID)
	if containerID == "" {
		return fmt.Errorf("container id/name is required")
	}

	actCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(actCtx, "podman", action, containerID)
	if out, err := cmd.CombinedOutput(); err != nil {
		if strings.TrimSpace(string(out)) != "" {
			return fmt.Errorf("podman %s %s failed: %s", action, containerID, strings.TrimSpace(string(out)))
		}
		return fmt.Errorf("podman %s %s failed: %w", action, containerID, err)
	}
	return nil
}

func listContainers(ctx context.Context) ([]viewdata.PodmanContainer, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(
		cmdCtx,
		"podman", "ps", "--all", "--format",
		"{{.ID}}|{{.Names}}|{{.Image}}|{{.State}}|{{.Status}}",
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 1 && strings.TrimSpace(lines[0]) == "" {
		return []viewdata.PodmanContainer{}, nil
	}

	containers := make([]viewdata.PodmanContainer, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 5)
		if len(parts) != 5 {
			continue
		}
		containers = append(containers, viewdata.PodmanContainer{
			ID:     strings.TrimSpace(parts[0]),
			Name:   strings.TrimSpace(parts[1]),
			Image:  strings.TrimSpace(parts[2]),
			State:  strings.TrimSpace(parts[3]),
			Status: strings.TrimSpace(parts[4]),
		})
	}
	return containers, nil
}

func listImages(ctx context.Context) ([]viewdata.PodmanImage, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(
		cmdCtx,
		"podman", "images", "--format",
		"{{.Repository}}|{{.Tag}}|{{.ID}}|{{.Size}}",
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 1 && strings.TrimSpace(lines[0]) == "" {
		return []viewdata.PodmanImage{}, nil
	}

	images := make([]viewdata.PodmanImage, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		if len(parts) != 4 {
			continue
		}
		images = append(images, viewdata.PodmanImage{
			Repository: strings.TrimSpace(parts[0]),
			Tag:        strings.TrimSpace(parts[1]),
			ID:         strings.TrimSpace(parts[2]),
			Size:       strings.TrimSpace(parts[3]),
		})
	}
	return images, nil
}
