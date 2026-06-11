//go:build darwin || linux || windows

package commands

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/wendylabsinc/wendy/go/internal/cli/tui"
	"github.com/wendylabsinc/wendy/go/internal/shared/config"
)

// stubEnrollPrompts replaces the interactive hooks used by the enrollment
// flow and restores them on cleanup. Each stub fails the test if invoked,
// so individual tests override only the hooks they expect to fire.
func stubEnrollPrompts(t *testing.T) {
	t.Helper()
	origSession := promptEnrollmentSession
	origPreEnroll := confirmPreEnroll
	origContinue := confirmContinueUnenrolled
	origEnroll := preEnrollDeviceFn
	promptEnrollmentSession = func([]tui.PickerItem) (string, error) {
		t.Fatal("unexpected session picker")
		return "", nil
	}
	confirmPreEnroll = func() (bool, error) {
		t.Fatal("unexpected pre-enroll confirm")
		return false, nil
	}
	confirmContinueUnenrolled = func() (bool, error) {
		t.Fatal("unexpected continue-unenrolled confirm")
		return false, nil
	}
	preEnrollDeviceFn = func(context.Context, *config.AuthConfig, string, PreEnrollDialer) ([]byte, error) {
		t.Fatal("unexpected enrollment call")
		return nil, nil
	}
	t.Cleanup(func() {
		promptEnrollmentSession = origSession
		confirmPreEnroll = origPreEnroll
		confirmContinueUnenrolled = origContinue
		preEnrollDeviceFn = origEnroll
	})
}

func twoSessionConfig() *config.Config {
	return &config.Config{Auth: []config.AuthConfig{
		{
			CloudDashboard: "https://cloud.wendy.sh",
			CloudGRPC:      "prod.example.com:443",
			Certificates:   []config.CertificateInfo{{OrganizationID: 7}},
		},
		{
			CloudDashboard: "http://localhost:3000",
			CloudGRPC:      "localhost:50051",
			Certificates:   []config.CertificateInfo{{OrganizationID: 1}},
		},
	}}
}

func TestSelectEnrollmentAuthNoSessions(t *testing.T) {
	stubEnrollPrompts(t)
	_, err := selectEnrollmentAuth(&config.Config{}, "", true)
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("expected not-logged-in error, got %v", err)
	}
}

func TestSelectEnrollmentAuthSingleSession(t *testing.T) {
	stubEnrollPrompts(t)
	cfg := &config.Config{Auth: []config.AuthConfig{{
		CloudGRPC:    "prod.example.com:443",
		Certificates: []config.CertificateInfo{{OrganizationID: 7}},
	}}}
	auth, err := selectEnrollmentAuth(cfg, "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if auth == nil || auth.CloudGRPC != "prod.example.com:443" {
		t.Fatalf("got %+v; want the single session", auth)
	}
}

func TestSelectEnrollmentAuthSingleSessionNoCerts(t *testing.T) {
	stubEnrollPrompts(t)
	cfg := &config.Config{Auth: []config.AuthConfig{{CloudGRPC: "prod.example.com:443"}}}
	_, err := selectEnrollmentAuth(cfg, "", true)
	if err == nil || !strings.Contains(err.Error(), "no certificates") {
		t.Fatalf("expected no-certificates error, got %v", err)
	}
}

func TestSelectEnrollmentAuthCloudGRPCFlag(t *testing.T) {
	stubEnrollPrompts(t)
	auth, err := selectEnrollmentAuth(twoSessionConfig(), "localhost:50051", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if auth == nil || auth.CloudGRPC != "localhost:50051" {
		t.Fatalf("got %+v; want the localhost session", auth)
	}
}

func TestSelectEnrollmentAuthCloudGRPCFlagNoMatch(t *testing.T) {
	stubEnrollPrompts(t)
	_, err := selectEnrollmentAuth(twoSessionConfig(), "missing.example.com:443", true)
	if err == nil || !strings.Contains(err.Error(), "no auth session for missing.example.com:443") {
		t.Fatalf("expected no-session error, got %v", err)
	}
}

func TestSelectEnrollmentAuthMultipleNonInteractive(t *testing.T) {
	stubEnrollPrompts(t)
	_, err := selectEnrollmentAuth(twoSessionConfig(), "", false)
	if err == nil || !strings.Contains(err.Error(), "--cloud-grpc") {
		t.Fatalf("expected multi-session error mentioning --cloud-grpc, got %v", err)
	}
}

func TestSelectEnrollmentAuthMultipleInteractivePicker(t *testing.T) {
	stubEnrollPrompts(t)
	var gotItems []tui.PickerItem
	promptEnrollmentSession = func(items []tui.PickerItem) (string, error) {
		gotItems = items
		return "1", nil // second session
	}

	auth, err := selectEnrollmentAuth(twoSessionConfig(), "", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if auth == nil || auth.CloudGRPC != "localhost:50051" {
		t.Fatalf("got %+v; want the localhost session", auth)
	}
	if len(gotItems) != 3 {
		t.Fatalf("picker should list 2 sessions + skip, got %d items", len(gotItems))
	}
	last := gotItems[len(gotItems)-1]
	if last.Value != skipEnrollmentValue {
		t.Fatalf("last picker item should be the skip option, got %+v", last)
	}
	if !strings.Contains(gotItems[0].Description, "org 7") {
		t.Errorf("session items should show the org id, got %q", gotItems[0].Description)
	}
}

func TestSelectEnrollmentAuthSkipOption(t *testing.T) {
	stubEnrollPrompts(t)
	promptEnrollmentSession = func([]tui.PickerItem) (string, error) {
		return skipEnrollmentValue, nil
	}
	auth, err := selectEnrollmentAuth(twoSessionConfig(), "", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if auth != nil {
		t.Fatalf("skip must return nil auth, got %+v", auth)
	}
}

func TestSelectEnrollmentAuthPickerCancelled(t *testing.T) {
	stubEnrollPrompts(t)
	promptEnrollmentSession = func([]tui.PickerItem) (string, error) {
		return "", ErrUserCancelled
	}
	_, err := selectEnrollmentAuth(twoSessionConfig(), "", true)
	if !errors.Is(err, ErrUserCancelled) {
		t.Fatalf("expected ErrUserCancelled, got %v", err)
	}
}

func TestResolvePreEnrollmentSkipMode(t *testing.T) {
	stubEnrollPrompts(t)
	js, err := resolvePreEnrollment(context.Background(), twoSessionConfig(), preEnrollOptions{mode: preEnrollSkip}, true, "dev")
	if err != nil || js != nil {
		t.Fatalf("skip mode must be a no-op, got %v / %v", js, err)
	}
}

func TestResolvePreEnrollmentAutoNonInteractive(t *testing.T) {
	stubEnrollPrompts(t)
	js, err := resolvePreEnrollment(context.Background(), twoSessionConfig(), preEnrollOptions{mode: preEnrollAuto}, false, "dev")
	if err != nil || js != nil {
		t.Fatalf("auto mode without a TTY must be a no-op, got %v / %v", js, err)
	}
}

func TestResolvePreEnrollmentAutoNoSessions(t *testing.T) {
	stubEnrollPrompts(t)
	js, err := resolvePreEnrollment(context.Background(), &config.Config{}, preEnrollOptions{mode: preEnrollAuto}, true, "dev")
	if err != nil || js != nil {
		t.Fatalf("auto mode without sessions must be a no-op, got %v / %v", js, err)
	}
}

func TestResolvePreEnrollmentAutoDeclined(t *testing.T) {
	stubEnrollPrompts(t)
	confirmPreEnroll = func() (bool, error) { return false, nil }
	js, err := resolvePreEnrollment(context.Background(), twoSessionConfig(), preEnrollOptions{mode: preEnrollAuto}, true, "dev")
	if err != nil || js != nil {
		t.Fatalf("declining the pre-enroll offer must be a no-op, got %v / %v", js, err)
	}
}

func TestResolvePreEnrollmentSuccess(t *testing.T) {
	stubEnrollPrompts(t)
	confirmPreEnroll = func() (bool, error) { return true, nil }
	promptEnrollmentSession = func([]tui.PickerItem) (string, error) { return "0", nil }
	preEnrollDeviceFn = func(_ context.Context, auth *config.AuthConfig, name string, _ PreEnrollDialer) ([]byte, error) {
		if auth.CloudGRPC != "prod.example.com:443" {
			t.Fatalf("enrolled against %s; want the picked session", auth.CloudGRPC)
		}
		if name != "dev" {
			t.Fatalf("device name %q; want dev", name)
		}
		return []byte(`{"enrolled":true}`), nil
	}

	js, err := resolvePreEnrollment(context.Background(), twoSessionConfig(), preEnrollOptions{mode: preEnrollAuto}, true, "dev")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(js) != `{"enrolled":true}` {
		t.Fatalf("got %q; want provisioning JSON", js)
	}
}

func TestResolvePreEnrollmentUserSkips(t *testing.T) {
	stubEnrollPrompts(t)
	confirmPreEnroll = func() (bool, error) { return true, nil }
	promptEnrollmentSession = func([]tui.PickerItem) (string, error) { return skipEnrollmentValue, nil }

	js, err := resolvePreEnrollment(context.Background(), twoSessionConfig(), preEnrollOptions{mode: preEnrollAuto}, true, "dev")
	if err != nil || js != nil {
		t.Fatalf("explicit skip must continue without JSON, got %v / %v", js, err)
	}
}

func TestResolvePreEnrollmentFailureAcknowledged(t *testing.T) {
	stubEnrollPrompts(t)
	confirmPreEnroll = func() (bool, error) { return true, nil }
	promptEnrollmentSession = func([]tui.PickerItem) (string, error) { return "0", nil }
	preEnrollDeviceFn = func(context.Context, *config.AuthConfig, string, PreEnrollDialer) ([]byte, error) {
		return nil, errors.New("cloud unreachable")
	}
	confirmContinueUnenrolled = func() (bool, error) { return true, nil }

	js, err := resolvePreEnrollment(context.Background(), twoSessionConfig(), preEnrollOptions{mode: preEnrollAuto}, true, "dev")
	if err != nil || js != nil {
		t.Fatalf("acknowledged failure must continue without JSON, got %v / %v", js, err)
	}
}

func TestResolvePreEnrollmentFailureDeclinedCancelsInstall(t *testing.T) {
	stubEnrollPrompts(t)
	confirmPreEnroll = func() (bool, error) { return true, nil }
	promptEnrollmentSession = func([]tui.PickerItem) (string, error) { return "0", nil }
	preEnrollDeviceFn = func(context.Context, *config.AuthConfig, string, PreEnrollDialer) ([]byte, error) {
		return nil, errors.New("cloud unreachable")
	}
	confirmContinueUnenrolled = func() (bool, error) { return false, nil }

	_, err := resolvePreEnrollment(context.Background(), twoSessionConfig(), preEnrollOptions{mode: preEnrollAuto}, true, "dev")
	if !errors.Is(err, ErrUserCancelled) {
		t.Fatalf("declining must cancel the install, got %v", err)
	}
}

func TestResolvePreEnrollmentForcedNonInteractiveFailureIsFatal(t *testing.T) {
	stubEnrollPrompts(t)
	cfg := &config.Config{Auth: []config.AuthConfig{{
		CloudGRPC:    "prod.example.com:443",
		Certificates: []config.CertificateInfo{{OrganizationID: 7}},
	}}}
	preEnrollDeviceFn = func(context.Context, *config.AuthConfig, string, PreEnrollDialer) ([]byte, error) {
		return nil, errors.New("cloud unreachable")
	}

	_, err := resolvePreEnrollment(context.Background(), cfg, preEnrollOptions{mode: preEnrollForced}, false, "dev")
	if err == nil || !strings.Contains(err.Error(), "--pre-enroll") {
		t.Fatalf("non-interactive --pre-enroll failure must be fatal, got %v", err)
	}
}

func TestResolvePreEnrollmentForcedNonInteractiveMultiSession(t *testing.T) {
	stubEnrollPrompts(t)
	_, err := resolvePreEnrollment(context.Background(), twoSessionConfig(), preEnrollOptions{mode: preEnrollForced}, false, "dev")
	if err == nil || !strings.Contains(err.Error(), "--cloud-grpc") {
		t.Fatalf("expected fatal multi-session error mentioning --cloud-grpc, got %v", err)
	}
}

func TestResolvePreEnrollmentForcedSkipsConfirm(t *testing.T) {
	stubEnrollPrompts(t)
	// confirmPreEnroll stays at the t.Fatal stub: forced mode must never ask
	// "Pre-enroll this device?".
	promptEnrollmentSession = func([]tui.PickerItem) (string, error) { return "0", nil }
	preEnrollDeviceFn = func(context.Context, *config.AuthConfig, string, PreEnrollDialer) ([]byte, error) {
		return []byte(`{}`), nil
	}
	if _, err := resolvePreEnrollment(context.Background(), twoSessionConfig(), preEnrollOptions{mode: preEnrollForced}, true, "dev"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolvePreEnrollmentAuthErrorInteractiveAsksToContinue(t *testing.T) {
	stubEnrollPrompts(t)
	// Single session without certificates: selection fails, user accepts
	// continuing unenrolled.
	cfg := &config.Config{Auth: []config.AuthConfig{{CloudGRPC: "prod.example.com:443"}}}
	confirmPreEnroll = func() (bool, error) { return true, nil }
	confirmContinueUnenrolled = func() (bool, error) { return true, nil }

	js, err := resolvePreEnrollment(context.Background(), cfg, preEnrollOptions{mode: preEnrollAuto}, true, "dev")
	if err != nil || js != nil {
		t.Fatalf("acknowledged auth failure must continue without JSON, got %v / %v", js, err)
	}
}
