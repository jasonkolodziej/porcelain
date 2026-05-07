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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jasonkolodziej/porcelain/porcelain/internal/modules"
	"github.com/jasonkolodziej/porcelain/porcelain/internal/viewdata"
)

const noteNotInstalled = "lm-sensors not installed"

// Module implements modules.Module for the Sensors page.
type Module struct {
	mode string
	note string

	mu         sync.Mutex
	history    map[string][]historyPoint
	thresholds map[string]float64
}

type historyPoint struct {
	At    time.Time
	Value float64
}

// ReadingHistoryPoint is a serializable snapshot point returned by History.
type ReadingHistoryPoint struct {
	Timestamp string  `json:"timestamp"`
	Value     float64 `json:"value"`
}

// New returns a Module that probes lm-sensors command availability.
func New(ctx context.Context) *Module {
	if _, err := exec.LookPath("sensors"); err != nil {
		return &Module{mode: "fake", note: noteNotInstalled, history: map[string][]historyPoint{}, thresholds: map[string]float64{}}
	}

	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, "sensors", "-u")
	if err := cmd.Run(); err != nil {
		if errors.Is(probeCtx.Err(), context.DeadlineExceeded) {
			return &Module{mode: "degraded", note: "sensors probe timed out", history: map[string][]historyPoint{}, thresholds: map[string]float64{}}
		}
		return &Module{mode: "degraded", note: "sensors command failed", history: map[string][]historyPoint{}, thresholds: map[string]float64{}}
	}

	return &Module{mode: "sensors", history: map[string][]historyPoint{}, thresholds: map[string]float64{}}
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
	m.applyThresholdsAndHistory(chips)
	data.Chips = chips
	data.Detail = fmt.Sprintf("%d chips, %d readings", len(chips), countReadings(chips))
	return data, nil
}

// SetThreshold sets an in-memory critical threshold for chip/reading.
func (m *Module) SetThreshold(chipName, readingName, value string) error {
	if m == nil {
		return fmt.Errorf("sensors module unavailable")
	}
	chipName = strings.TrimSpace(chipName)
	readingName = strings.TrimSpace(readingName)
	if chipName == "" || readingName == "" {
		return fmt.Errorf("chip and reading are required")
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return fmt.Errorf("invalid threshold value: %w", err)
	}
	m.mu.Lock()
	m.thresholds[historyKey(chipName, readingName)] = v
	m.mu.Unlock()
	return nil
}

// ClearThreshold removes an in-memory critical threshold override.
func (m *Module) ClearThreshold(chipName, readingName string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	delete(m.thresholds, historyKey(chipName, readingName))
	m.mu.Unlock()
}

// History returns recent points for a chip/reading pair.
func (m *Module) History(chipName, readingName string, limit int) []ReadingHistoryPoint {
	if m == nil {
		return nil
	}
	if limit <= 0 {
		limit = 30
	}
	m.mu.Lock()
	points := append([]historyPoint(nil), m.history[historyKey(chipName, readingName)]...)
	m.mu.Unlock()

	if len(points) > limit {
		points = points[len(points)-limit:]
	}
	out := make([]ReadingHistoryPoint, 0, len(points))
	for _, p := range points {
		out = append(out, ReadingHistoryPoint{Timestamp: p.At.UTC().Format(time.RFC3339), Value: p.Value})
	}
	return out
}

func (m *Module) applyThresholdsAndHistory(chips []viewdata.SensorChip) {
	if m == nil {
		return
	}
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()

	for cIdx := range chips {
		chip := &chips[cIdx]
		for rIdx := range chip.Readings {
			reading := &chip.Readings[rIdx]
			parsed, err := strconv.ParseFloat(reading.Value, 64)
			if err != nil {
				continue
			}

			key := historyKey(chip.Name, reading.Name)
			if thr, ok := m.thresholds[key]; ok {
				reading.Threshold = formatValue(thr)
				reading.Critical = parsed >= thr
			}

			series := append(m.history[key], historyPoint{At: now, Value: parsed})
			if len(series) > 240 {
				series = series[len(series)-240:]
			}
			m.history[key] = series
		}
	}
}

func historyKey(chipName, readingName string) string {
	return chipName + "::" + readingName
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
