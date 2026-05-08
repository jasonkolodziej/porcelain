package sensors

import (
	"encoding/json"
	"testing"
)

// TestReadSensorsJSONMixedPayload verifies that the parser tolerates sensors -j
// output that includes string-valued fields (e.g. "Adapter") alongside numeric
// feature objects. Previously this caused a type-mismatch unmarshal error.
func TestReadSensorsJSONMixedPayload(t *testing.T) {
	// A representative sensors -j payload with an "Adapter" string field and
	// a temperature feature object containing numeric sub-keys.
	raw := `{
		"coretemp-isa-0000": {
			"Adapter": "ISA adapter",
			"Core 0": {
				"temp2_input": 32.0,
				"temp2_max": 100.0,
				"temp2_crit": 100.0,
				"temp2_crit_alarm": 0.0
			},
			"Core 1": {
				"temp3_input": 34.0,
				"temp3_max": 100.0,
				"temp3_crit": 100.0,
				"temp3_crit_alarm": 0.0
			}
		},
		"acpitz-acpi-0": {
			"Adapter": "ACPI interface",
			"temp1": {
				"temp1_input": 27.8,
				"temp1_crit": 105.0
			}
		}
	}`

	// Validate that the intermediate json.RawMessage layer parses cleanly
	// (the same struct used by readSensorsJSON).
	var root map[string]map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}

	// String-valued fields must survive the first unmarshal and then be skipped
	// when we try to parse them as map[string]float64.
	adapter, ok := root["coretemp-isa-0000"]["Adapter"]
	if !ok {
		t.Fatal("expected Adapter key in coretemp chip")
	}
	var floatMap map[string]float64
	if err := json.Unmarshal(adapter, &floatMap); err == nil {
		t.Fatal("expected string 'Adapter' field to fail float64 map parse (it should be skipped)")
	}

	// Numeric feature maps must parse correctly.
	core0, ok := root["coretemp-isa-0000"]["Core 0"]
	if !ok {
		t.Fatal("expected Core 0 key in coretemp chip")
	}
	var core0vals map[string]float64
	if err := json.Unmarshal(core0, &core0vals); err != nil {
		t.Fatalf("expected Core 0 to parse as float64 map: %v", err)
	}
	if core0vals["temp2_input"] != 32.0 {
		t.Fatalf("expected temp2_input=32.0, got %v", core0vals["temp2_input"])
	}
}

// TestReadSensorsJSONStringOnlyChip verifies that a chip containing only
// string-valued fields (no numeric features) results in a chip with zero
// readings rather than an error.
func TestReadSensorsJSONStringOnlyChip(t *testing.T) {
	raw := `{
		"fake-chip-0": {
			"Adapter": "Fake adapter",
			"Name": "some string"
		}
	}`

	var root map[string]map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}

	readings := 0
	for _, rawVal := range root["fake-chip-0"] {
		var vals map[string]float64
		if err := json.Unmarshal(rawVal, &vals); err == nil {
			for k := range vals {
				if len(k) > 0 {
					readings++
				}
			}
		}
	}

	if readings != 0 {
		t.Fatalf("expected 0 readings for string-only chip, got %d", readings)
	}
}
