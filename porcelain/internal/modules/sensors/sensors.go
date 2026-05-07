// Package sensors exposes a sidebar-visible Module summarising lm-sensors
// availability.
package sensors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/jasonkolodziej/porcelain/porcelain/internal/modules"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/viewdata"
)

const noteNotInstalled = "lm-sensors not installed"

// Module implements modules.Module for the Sensors page.
type Module struct {
	mode string
	note string
}

// New returns a Module that probes lm-sensors command availability.
func New(ctx context.Context) *Module {
	if _, err := exec.LookPath("sensors"); err != nil {
		return &Module{mode: "fake", note: noteNotInstalled}
	}

	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, "sensors", "-u")
	if err := cmd.Run(); err != nil {
		if errors.Is(probeCtx.Err(), context.DeadlineExceeded) {
			return &Module{mode: "degraded", note: "sensors probe timed out"}
		}
		return &Module{mode: "degraded", note: "sensors command failed"}
	}

	return &Module{mode: "sensors"}
}

// ID implements modules.Module.
func (m *Module) ID() string { return "sensors" }

// Name implements modules.Module.
func (m *Module) Name() string { return "Sensors" }

// Status implements modules.Module.
func (m *Module) Status(_ context.Context) modules.Status {
	switch m.mode {
	case "sensors":
		return modules.Status{Health: modules.HealthOK, Detail: "lm-sensors available"}
	case "degraded":
		detail := m.note
		if detail == "" {
			detail = "lm-sensors partially available"
		}
		return modules.Status{Health: modules.HealthDegraded, Detail: detail}
	case "fake":
		detail := m.note
		if detail == "" {
			detail = noteNotInstalled
		}
		return modules.Status{Health: modules.HealthDegraded, Detail: detail}
	default:
		return modules.Status{Health: modules.HealthUnavailable}
	}
}

// Close implements modules.Module.
func (m *Module) Close() error { return nil }

// Snapshot returns sensor telemetry for the sensors page.
func (m *Module) Snapshot(ctx context.Context) (viewdata.SensorsPageData, error) {
	data := viewdata.SensorsPageData{
		Heading: "Sensors",
		Blurb:   "Hardware telemetry via lm-sensors",
	}

	if m == nil || m.mode == "fake" {
		if m != nil && m.note != "" {
			data.Detail = m.note
		} else {
			data.Detail = noteNotInstalled
		}
		return data, nil
	}
	if m.mode == "degraded" {
		data.Detail = m.note
		return data, nil
	}

	chips, err := readSensorsJSON(ctx)
	if err != nil {
		return data, err
	}
	data.Chips = chips
	data.Detail = fmt.Sprintf("%d chips, %d readings", len(chips), countReadings(chips))
	return data, nil
}

func readSensorsJSON(ctx context.Context) ([]viewdata.SensorChip, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, "sensors", "-j")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("sensors -j failed: %w", err)
	}

	var root map[string]map[string]map[string]float64
	if err := json.Unmarshal(out, &root); err != nil {
		return nil, fmt.Errorf("parse sensors json: %w", err)
	}

	chipNames := make([]string, 0, len(root))
	for chip := range root {
		chipNames = append(chipNames, chip)
	}
	sort.Strings(chipNames)

	chips := make([]viewdata.SensorChip, 0, len(chipNames))
	for _, chipName := range chipNames {
		featureMap := root[chipName]
		featureNames := make([]string, 0, len(featureMap))
		for f := range featureMap {
			featureNames = append(featureNames, f)
		}
		sort.Strings(featureNames)

		readings := make([]viewdata.SensorReading, 0)
		for _, featureName := range featureNames {
			vals := featureMap[featureName]
			input := firstValueBySuffix(vals, "_input")
			if input == nil {
				continue
			}
			high := firstValueBySuffix(vals, "_max", "_high")
			crit := firstValueBySuffix(vals, "_crit", "_emergency")

			unit := inferUnit(featureName)
			isCritical := false
			if crit != nil && *input >= *crit {
				isCritical = true
			}

			reading := viewdata.SensorReading{
				Name:      featureName,
				Value:     formatValue(*input),
				Unit:      unit,
				Critical:  isCritical,
				Threshold: valueOrEmpty(crit),
				High:      valueOrEmpty(high),
			}
			readings = append(readings, reading)
		}

		chips = append(chips, viewdata.SensorChip{Name: chipName, Readings: readings})
	}

	return chips, nil
}

func firstValueBySuffix(vals map[string]float64, suffixes ...string) *float64 {
	for _, sfx := range suffixes {
		for key, v := range vals {
			if strings.HasSuffix(key, sfx) {
				copy := v
				return &copy
			}
		}
	}
	return nil
}

func inferUnit(name string) string {
	ln := strings.ToLower(name)
	switch {
	case strings.Contains(ln, "temp"):
		return "degC"
	case strings.Contains(ln, "fan"):
		return "RPM"
	case strings.Contains(ln, "in") || strings.Contains(ln, "volt"):
		return "V"
	case strings.Contains(ln, "power"):
		return "W"
	case strings.Contains(ln, "curr"):
		return "A"
	default:
		return ""
	}
}

func formatValue(v float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", v), "0"), ".")
}

func valueOrEmpty(v *float64) string {
	if v == nil {
		return ""
	}
	return formatValue(*v)
}

func countReadings(chips []viewdata.SensorChip) int {
	total := 0
	for _, c := range chips {
		total += len(c.Readings)
	}
	return total
}
