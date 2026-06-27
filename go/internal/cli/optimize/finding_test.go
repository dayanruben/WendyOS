package optimize

import "testing"

func TestSeverityString(t *testing.T) {
	cases := []struct {
		sev  Severity
		want string
	}{
		{SeverityInfo, "info"},
		{SeverityWarning, "warning"},
		{SeverityError, "error"},
	}
	for _, c := range cases {
		t.Run(c.want, func(t *testing.T) {
			if got := c.sev.String(); got != c.want {
				t.Fatalf("String() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestParseSeverity(t *testing.T) {
	got, err := ParseSeverity("warning")
	if err != nil {
		t.Fatalf("ParseSeverity returned error: %v", err)
	}
	if got != SeverityWarning {
		t.Fatalf("ParseSeverity = %v, want SeverityWarning", got)
	}
	if _, err := ParseSeverity("bogus"); err == nil {
		t.Fatalf("ParseSeverity(\"bogus\") expected error, got nil")
	}
}
