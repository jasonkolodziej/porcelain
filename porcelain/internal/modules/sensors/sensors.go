// Package sensors exposes a sidebar-visible Module summarising lm-sensors
// availability.
package sensors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	path string

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

type persistedState struct {
	History    map[string][]persistedPoint `json:"history"`
	Thresholds map[string]float64          `json:"thresholds"`
}

type persistedPoint struct {
	AtUnix int64   `json:"at_unix"`
	Value  float64 `json:"value"`
}

// New returns a Module that probes lm-sensors command availability.
func New(ctx context.Context) *Module {
	statePath := defaultStatePath()
	newModule := func(mode, note string) *Module {
		m := &Module{
			mode:       mode,
			note:       note,
			path:       statePath,
			history:    map[string][]historyPoint{},
			thresholds: map[string]float64{},
		}
		m.loadState()
		return m
	}

	if _, err := exec.LookPath("sensors"); err != nil {
		return newModule("fake", noteNotInstalled)
	}

	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, "sensors", "-u")
	if err := cmd.Run(); err != nil {
		if errors.Is(probeCtx.Err(), context.DeadlineExceeded) {
			return newModule("degraded", "sensors probe timed out")
		}
		return newModule("degraded", "sensors command failed")
	}

	return newModule("sensors", "")
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
	m.saveStateLocked()
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
	m.saveStateLocked()
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

	m.saveStateLocked()
}

func historyKey(chipName, readingName string) string {
	return chipName + "::" + readingName
}

func (m *Module) loadState() {
	if m == nil || strings.TrimSpace(m.path) == "" {
		return
	}
	raw, err := os.ReadFile(m.path)
	if err != nil {
		return
	}
	var state persistedState
	if err := json.Unmarshal(raw, &state); err != nil {
		return
	}
	if state.Thresholds != nil {
		m.thresholds = state.Thresholds
	}
	if state.History != nil {
		m.history = make(map[string][]historyPoint, len(state.History))
		for key, points := range state.History {
			series := make([]historyPoint, 0, len(points))
			for _, p := range points {
				series = append(series, historyPoint{At: time.Unix(p.AtUnix, 0).UTC(), Value: p.Value})
			}
			m.history[key] = series
		}
	}
}

func (m *Module) saveStateLocked() {
	if m == nil || strings.TrimSpace(m.path) == "" {
		return
	}
	state := persistedState{
		History:    make(map[string][]persistedPoint, len(m.history)),
		Thresholds: make(map[string]float64, len(m.thresholds)),
	}
	for key, val := range m.thresholds {
		state.Thresholds[key] = val
	}
	for key, points := range m.history {
		series := make([]persistedPoint, 0, len(points))
		for _, p := range points {
			series = append(series, persistedPoint{AtUnix: p.At.Unix(), Value: p.Value})
		}
		state.History[key] = series
	}

	raw, err := json.Marshal(state)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(m.path, raw, 0o600)
}

func defaultStatePath() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".local", "state", "porcelain", "sensors-state.json")
}

func readSensorsJSON(ctx context.Context) ([]viewdata.SensorChip, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, "sensors", "-j")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("sensors -j failed: %w", err)
	}

	// sensors -j includes string fields like "Adapter" alongside numeric feature
	// objects. Use json.RawMessage at each level and skip non-object entries.
	var root map[string]map[string]json.RawMessage
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
		rawFeatureMap := root[chipName]
		featureNames := make([]string, 0, len(rawFeatureMap))
		for f := range rawFeatureMap {
			featureNames = append(featureNames, f)
		}
		sort.Strings(featureNames)

		readings := make([]viewdata.SensorReading, 0)
		for _, featureName := range featureNames {
			// sensors -j includes per-chip metadata fields (e.g. "Adapter": "ISA adapter")
			// alongside numeric feature objects. These metadata fields are strings, not
			// sensor readings, so skip any entry that does not unmarshal as a float64 map.
			var vals map[string]float64
			if err := json.Unmarshal(rawFeatureMap[featureName], &vals); err != nil {
				continue
			}

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
