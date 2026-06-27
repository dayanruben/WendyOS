package optimize

import (
	"strings"
	"testing"
)

func sampleReport() Report {
	targets := []Target{{Name: "app", Kind: KindDockerfile}}
	findings := []Finding{
		{Analyzer: "arch-image", Severity: SeverityError, Title: "amd64 base", Location: &Loc{File: "Dockerfile", Line: 1}},
		{Analyzer: "build-cache", Severity: SeverityWarning, Title: "no cache", Location: &Loc{File: "Dockerfile", Line: 4}, Fix: &Fix{Op: FixReplaceLine}},
		{Analyzer: "arch-image", Severity: SeverityWarning, Title: "No .dockerignore", Fix: &Fix{Op: FixCreateFile}},
	}
	return BuildReport(targets, findings)
}

func TestCountsAndMaxSeverity(t *testing.T) {
	r := sampleReport()
	info, warn, errc, fixable := r.Counts()
	if info != 0 || warn != 2 || errc != 1 || fixable != 2 {
		t.Fatalf("counts = info:%d warn:%d err:%d fixable:%d", info, warn, errc, fixable)
	}
	if r.MaxSeverity() != SeverityError {
		t.Fatalf("MaxSeverity = %v, want error", r.MaxSeverity())
	}
}

func TestRenderHuman(t *testing.T) {
	out := RenderHuman(sampleReport())
	if !strings.Contains(out, "app (dockerfile)") {
		t.Fatalf("missing target header:\n%s", out)
	}
	if !strings.Contains(out, "build-cache:4") || !strings.Contains(out, "(fixable)") {
		t.Fatalf("missing fixable build-cache line:\n%s", out)
	}
	if strings.Contains(out, "arch-image:0") {
		t.Fatalf("location-less finding should not print :0\n%s", out)
	}
}
