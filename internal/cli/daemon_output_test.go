package cli

import (
	"bytes"
	"strings"
	"testing"
)

func captureDaemonOutput(t *testing.T, fn func() error) string {
	t.Helper()
	previous := output
	var buf bytes.Buffer
	output = &buf
	defer func() {
		output = previous
	}()
	if err := fn(); err != nil {
		t.Fatalf("print failed: %v", err)
	}
	return strings.TrimSpace(buf.String())
}

func TestDaemonStartOutput(t *testing.T) {
	got := captureDaemonOutput(t, func() error {
		return printDaemonStart(false)
	})
	if got != "real browser daemon start successfully." {
		t.Fatalf("unexpected start output: %q", got)
	}

	got = captureDaemonOutput(t, func() error {
		return printDaemonStart(true)
	})
	if got != "real browser daemon has already started." {
		t.Fatalf("unexpected already-started output: %q", got)
	}

	got = captureDaemonOutput(t, printDaemonStartFailed)
	if got != "real browser daemon start failed." {
		t.Fatalf("unexpected start-failed output: %q", got)
	}
}

func TestDaemonStatusOutput(t *testing.T) {
	got := captureDaemonOutput(t, func() error {
		return printDaemonStatus(daemonHealth{})
	})
	if got != "real browser daemon is not running." {
		t.Fatalf("unexpected not-running output: %q", got)
	}

	got = captureDaemonOutput(t, func() error {
		return printDaemonStatus(daemonHealth{Running: true, Ready: true})
	})
	want := strings.Join([]string{
		"real browser daemon is running.",
		"browser plugin is connected.",
	}, "\n")
	if got != want {
		t.Fatalf("unexpected connected status output: %q", got)
	}

	got = captureDaemonOutput(t, func() error {
		return printDaemonStatus(daemonHealth{Running: true, Ready: false})
	})
	want = strings.Join([]string{
		"real browser daemon is running.",
		"browser plugin is not connected.",
	}, "\n")
	if got != want {
		t.Fatalf("unexpected disconnected status output: %q", got)
	}
}

func TestDaemonStopOutput(t *testing.T) {
	got := captureDaemonOutput(t, printDaemonStopSuccess)
	if got != "real browser daemon stop successfully." {
		t.Fatalf("unexpected stop output: %q", got)
	}

	got = captureDaemonOutput(t, printDaemonStopFailed)
	if got != "real browser daemon stop failed." {
		t.Fatalf("unexpected stop-failed output: %q", got)
	}
}

func TestDaemonRestartOutput(t *testing.T) {
	got := captureDaemonOutput(t, func() error {
		return printDaemonRestartSuccess(daemonHealth{Running: true, Ready: true})
	})
	want := strings.Join([]string{
		"real browser daemon restart successfully.",
		"browser plugin is connected.",
	}, "\n")
	if got != want {
		t.Fatalf("unexpected restart output: %q", got)
	}

	got = captureDaemonOutput(t, func() error {
		return printDaemonRestartSuccess(daemonHealth{Running: true, Ready: false})
	})
	want = strings.Join([]string{
		"real browser daemon restart successfully.",
		"browser plugin is not connected.",
	}, "\n")
	if got != want {
		t.Fatalf("unexpected restart disconnected output: %q", got)
	}

	got = captureDaemonOutput(t, printDaemonRestartFailed)
	if got != "real browser daemon restart failed." {
		t.Fatalf("unexpected restart-failed output: %q", got)
	}
}

func TestParseDaemonHealth(t *testing.T) {
	health, err := parseDaemonHealth([]byte(`{"running":true,"ready":true}`))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if !health.Running || !health.Ready {
		t.Fatalf("unexpected health: %+v", health)
	}
}
