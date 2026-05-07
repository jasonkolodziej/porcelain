// Package podman exposes a sidebar-visible Module summarising local Podman
// availability and basic runtime reachability.
package podman

import (
	"context"
	"encoding/json"
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

// ContainerLogs returns the last tail lines from the named container. tail=0
// defaults to 100. The output is plain text (no ANSI sequences).
func (m *Module) ContainerLogs(ctx context.Context, containerID string, tail int) (string, error) {
	if m == nil || m.mode == "fake" {
		return "", fmt.Errorf("podman not installed")
	}
	containerID = strings.TrimSpace(containerID)
	if containerID == "" {
		return "", fmt.Errorf("container id/name is required")
	}
	if tail <= 0 {
		tail = 100
	}

	logCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(logCtx, "podman", "logs", "--tail", fmt.Sprintf("%d", tail), containerID)
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if text != "" {
			return "", fmt.Errorf("podman logs failed: %s", text)
		}
		return "", fmt.Errorf("podman logs failed: %w", err)
	}
	return string(out), nil
}

// InspectContainer returns normalized JSON for podman inspect <container>.
func (m *Module) InspectContainer(ctx context.Context, containerID string) (string, error) {
	if m == nil || m.mode == "fake" {
		return "", fmt.Errorf("podman not installed")
	}
	containerID = strings.TrimSpace(containerID)
	if containerID == "" {
		return "", fmt.Errorf("container id/name is required")
	}

	inspectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(inspectCtx, "podman", "inspect", containerID)
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if text != "" {
			return "", fmt.Errorf("podman inspect failed: %s", text)
		}
		return "", fmt.Errorf("podman inspect failed: %w", err)
	}

	var parsed any
	if err := json.Unmarshal(out, &parsed); err != nil {
		return "", fmt.Errorf("parse inspect json: %w", err)
	}
	normalized, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode inspect json: %w", err)
	}
	return string(normalized), nil
}

// InspectImage returns normalized JSON for podman image inspect <image>.
func (m *Module) InspectImage(ctx context.Context, imageRef string) (string, error) {
	if m == nil || m.mode == "fake" {
		return "", fmt.Errorf("podman not installed")
	}
	imageRef = strings.TrimSpace(imageRef)
	if imageRef == "" {
		return "", fmt.Errorf("image reference is required")
	}

	inspectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(inspectCtx, "podman", "image", "inspect", imageRef)
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if text != "" {
			return "", fmt.Errorf("podman image inspect failed: %s", text)
		}
		return "", fmt.Errorf("podman image inspect failed: %w", err)
	}

	var parsed any
	if err := json.Unmarshal(out, &parsed); err != nil {
		return "", fmt.Errorf("parse image inspect json: %w", err)
	}
	normalized, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode image inspect json: %w", err)
	}
	return string(normalized), nil
}

// Exec runs a command in a running container and returns combined output.
func (m *Module) Exec(ctx context.Context, containerID string, command []string) (string, error) {
	if m == nil || m.mode == "fake" {
		return "", fmt.Errorf("podman not installed")
	}
	containerID = strings.TrimSpace(containerID)
	if containerID == "" {
		return "", fmt.Errorf("container id/name is required")
	}
	if len(command) == 0 {
		return "", fmt.Errorf("exec command is required")
	}

	args := []string{"exec", containerID}
	args = append(args, command...)

	execCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(execCtx, "podman", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if text != "" {
			return "", fmt.Errorf("podman exec failed: %s", text)
		}
		return "", fmt.Errorf("podman exec failed: %w", err)
	}
	return string(out), nil
}

// PullImage pulls an image from a registry.
func (m *Module) PullImage(ctx context.Context, imageRef string) (string, error) {
	if m == nil || m.mode == "fake" {
		return "", fmt.Errorf("podman not installed")
	}
	imageRef = strings.TrimSpace(imageRef)
	if imageRef == "" {
		return "", fmt.Errorf("image reference is required")
	}

	pullCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(pullCtx, "podman", "pull", imageRef)
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if text != "" {
			return "", fmt.Errorf("podman pull failed: %s", text)
		}
		return "", fmt.Errorf("podman pull failed: %w", err)
	}
	return string(out), nil
}

// BuildImage builds a local image from a context path and optional Dockerfile.
func (m *Module) BuildImage(ctx context.Context, contextPath, dockerfilePath, tag string) (string, error) {
	if m == nil || m.mode == "fake" {
		return "", fmt.Errorf("podman not installed")
	}
	contextPath = strings.TrimSpace(contextPath)
	if contextPath == "" {
		return "", fmt.Errorf("build context path is required")
	}

	args := []string{"build"}
	if strings.TrimSpace(dockerfilePath) != "" {
		args = append(args, "-f", strings.TrimSpace(dockerfilePath))
	}
	if strings.TrimSpace(tag) != "" {
		args = append(args, "-t", strings.TrimSpace(tag))
	}
	args = append(args, contextPath)

	buildCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(buildCtx, "podman", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if text != "" {
			return "", fmt.Errorf("podman build failed: %s", text)
		}
		return "", fmt.Errorf("podman build failed: %w", err)
	}
	return string(out), nil
}

// RegistryLogin authenticates podman to a remote registry.
func (m *Module) RegistryLogin(ctx context.Context, registry, username, password string) error {
	if m == nil || m.mode == "fake" {
		return fmt.Errorf("podman not installed")
	}
	registry = strings.TrimSpace(registry)
	username = strings.TrimSpace(username)
	if registry == "" || username == "" || password == "" {
		return fmt.Errorf("registry, username, and password are required")
	}

	loginCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(loginCtx, "podman", "login", "--username", username, "--password-stdin", registry)
	cmd.Stdin = strings.NewReader(password)
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if text != "" {
			return fmt.Errorf("podman login failed: %s", text)
		}
		return fmt.Errorf("podman login failed: %w", err)
	}
	return nil
}

// RegistryLogout removes stored credentials for a registry.
func (m *Module) RegistryLogout(ctx context.Context, registry string) error {
	if m == nil || m.mode == "fake" {
		return fmt.Errorf("podman not installed")
	}
	registry = strings.TrimSpace(registry)
	if registry == "" {
		return fmt.Errorf("registry is required")
	}

	logoutCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(logoutCtx, "podman", "logout", registry)
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if text != "" {
			return fmt.Errorf("podman logout failed: %s", text)
		}
		return fmt.Errorf("podman logout failed: %w", err)
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
