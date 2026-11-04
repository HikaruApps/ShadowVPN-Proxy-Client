package main

import (
	"errors"
	"strings"
	"testing"
)

func TestDiagnoseCandidates(t *testing.T) {
	profiles := []Profile{{ID: "a", Name: "🇩🇪 Germany"}, {ID: "b", Name: "🇷🇺 Russia"}, {ID: "c", Name: "Finland"}, {ID: "d"}, {ID: "e"}, {ID: "f"}}
	if got, err := diagnoseCandidates(profiles, "b", nil); err != nil || len(got) != 1 || got[0].ID != "b" {
		t.Fatalf("single = %v, %v", got, err)
	}
	if got, _ := diagnoseCandidates(profiles, autoNoRUProfileID, []string{"a", "b", "c"}); len(got) != 2 || got[1].ID != "c" {
		t.Fatalf("auto without RU = %v", got)
	}
	if got, _ := diagnoseCandidates(profiles, autoProfileID, nil); len(got) != 4 {
		t.Fatalf("auto should check at most 4 candidates, got %d", len(got))
	}
	if _, err := diagnoseCandidates(profiles, "missing", nil); err == nil {
		t.Fatal("missing profile accepted")
	}
}

func TestDiagnoseMasksAddresses(t *testing.T) {
	if got := maskIP("203.0.113.77"); got != "203.0.*.*" {
		t.Fatalf("maskIP = %q", got)
	}
	if got := maskIP("2001:db8:1:2::5"); got != "2001:db8:*:*" {
		t.Fatalf("maskIP v6 = %q", got)
	}
	detail := maskIPs(explainProbeError(errors.New("dial tcp 203.0.113.77:443: connect: connection refused")))
	if strings.Contains(detail, "113.77") || !strings.Contains(detail, "отклонил") {
		t.Fatalf("probe error = %q", detail)
	}
}
