package optimize

import (
	"fmt"
	"strings"
)

// ReportTarget is the lightweight target view embedded in a report.
type ReportTarget struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// Report is the rendered/serialized output of an optimize run.
type Report struct {
	Targets  []ReportTarget `json:"targets"`
	Findings []Finding      `json:"findings"`
}

// BuildReport assembles a Report from targets and findings.
func BuildReport(targets []Target, findings []Finding) Report {
	rt := make([]ReportTarget, 0, len(targets))
	for _, t := range targets {
		rt = append(rt, ReportTarget{Name: t.Name, Kind: t.Kind.String()})
	}
	return Report{Targets: rt, Findings: findings}
}

// Counts returns finding counts by severity plus the number of fixable findings.
func (r Report) Counts() (info, warning, errc, fixable int) {
	for _, f := range r.Findings {
		switch f.Severity {
		case SeverityInfo:
			info++
		case SeverityWarning:
			warning++
		case SeverityError:
			errc++
		}
		if f.Fix != nil {
			fixable++
		}
	}
	return
}

// MaxSeverity returns the highest severity in the report (SeverityInfo if empty).
func (r Report) MaxSeverity() Severity {
	maxSev := SeverityInfo
	for _, f := range r.Findings {
		if f.Severity > maxSev {
			maxSev = f.Severity
		}
	}
	return maxSev
}

// RenderHuman renders a plain-text report grouped by target Name.
func RenderHuman(r Report) string {
	var b strings.Builder

	for _, t := range r.Targets {
		fmt.Fprintf(&b, "%s (%s)\n", t.Name, t.Kind)
		for _, f := range r.Findings {
			if f.Target == t.Name {
				writeFindingLine(&b, f)
			}
		}
	}
	// Defensive: any finding whose Target matches no listed target still gets shown.
	for _, f := range r.Findings {
		matched := false
		for _, t := range r.Targets {
			if f.Target == t.Name {
				matched = true
				break
			}
		}
		if !matched {
			writeFindingLine(&b, f)
		}
	}

	info, warn, errc, fixable := r.Counts()
	total := info + warn + errc
	fmt.Fprintf(&b, "\n%d findings (%d errors, %d warnings, %d info)", total, errc, warn, info)
	if fixable > 0 {
		fmt.Fprintf(&b, "  ·  %d fixable — run with --fix", fixable)
	}
	b.WriteString("\n")
	return b.String()
}

func writeFindingLine(b *strings.Builder, f Finding) {
	loc := ""
	if f.Location != nil {
		loc = fmt.Sprintf(":%d", f.Location.Line)
	}
	fixable := ""
	if f.Fix != nil {
		fixable = "  (fixable)"
	}
	fmt.Fprintf(b, "  %-7s  %s%s  %s%s\n", f.Severity.String(), f.Analyzer, loc, f.Title, fixable)
}
