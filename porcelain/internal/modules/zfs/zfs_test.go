package zfs

import (
	"strings"
	"testing"
)

func TestParseScrubProgress_InProgress(t *testing.T) {
	raw := strings.Join([]string{
		"  pool: tank",
		" state: ONLINE",
		"  scan: scrub in progress since Wed May  7 10:32:19 2026",
		"        6.73T / 10.20T scanned at 1.12G/s, 0B / 10.20T issued at 0B/s, 0B repaired, 65.98% done, 0 days 00:52:13 to go",
	}, "\n")

	progress := parseScrubProgress("tank", raw)
	if !progress.InProgress {
		t.Fatal("expected scrub to be in progress")
	}
	if progress.State != "running" {
		t.Fatalf("state: got %q", progress.State)
	}
	if progress.Percent < 65.9 || progress.Percent > 66.1 {
		t.Fatalf("percent: got %.2f", progress.Percent)
	}
	if progress.ETA != "0 days 00:52:13" {
		t.Fatalf("eta: got %q", progress.ETA)
	}
	if got := progress.taskMessage(); !strings.Contains(got, "65.98% done") || !strings.Contains(got, "0 days 00:52:13 to go") {
		t.Fatalf("task message: got %q", got)
	}
	status, detail := progress.statusAndTimestamp()
	if status != "scrub 66%" {
		t.Fatalf("status: got %q", status)
	}
	if detail != "0 days 00:52:13 to go" {
		t.Fatalf("detail: got %q", detail)
	}
}

func TestParseScrubProgress_Completed(t *testing.T) {
	raw := "  scan: scrub repaired 0B in 03:12:45 with 0 errors on Wed May  7 09:01:02 2026\n"

	progress := parseScrubProgress("tank", raw)
	if progress.InProgress {
		t.Fatal("expected completed scrub to be non-running")
	}
	if progress.State != "done" {
		t.Fatalf("state: got %q", progress.State)
	}
	if progress.completionMessage() == "" {
		t.Fatal("expected completion message")
	}
	status, detail := progress.statusAndTimestamp()
	if status != "scrub repaired 0B in 03:12:45 with 0 errors" {
		t.Fatalf("status: got %q", status)
	}
	if detail != "Wed May  7 09:01:02 2026" {
		t.Fatalf("detail: got %q", detail)
	}
}