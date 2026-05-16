package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func captureRPCOutput(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	previous := output
	var buf bytes.Buffer
	output = &buf
	defer func() {
		output = previous
	}()
	err := fn()
	return strings.TrimSpace(buf.String()), err
}

func TestPrintRPCDefaultContent(t *testing.T) {
	globals.JSON = false
	globals.Quiet = false
	got, err := captureRPCOutput(t, func() error {
		return printRPCDefault("get", []byte(`{"id":"1","success":true,"data":{"title":"Example"},"meta":{"command":"get"}}`))
	})
	if err != nil {
		t.Fatalf("print failed: %v", err)
	}
	if got != "Example" {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestPrintRPCDefaultFailure(t *testing.T) {
	globals.JSON = false
	globals.Quiet = false
	got, err := captureRPCOutput(t, func() error {
		return printRPCDefault("action.click", []byte(`{"id":"1","success":false,"error":{"code":"target_not_found","message":"target not found: @e4","retryable":false,"details":{}}}`))
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if got != "" {
		t.Fatalf("failure should not print stdout: %q", got)
	}
	if err.Error() != "target_not_found: target not found: @e4" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPrintRPCQuietPath(t *testing.T) {
	globals.JSON = false
	globals.Quiet = true
	defer func() { globals.Quiet = false }()
	got, err := captureRPCOutput(t, func() error {
		return printRPCDefault("screenshot", []byte(`{"id":"1","success":true,"data":{"path":"/tmp/page.png"},"meta":{"command":"screenshot"}}`))
	})
	if err != nil {
		t.Fatalf("print failed: %v", err)
	}
	if got != "/tmp/page.png" {
		t.Fatalf("unexpected quiet output: %q", got)
	}
}

func TestPrintLocalResponsePluginJSON(t *testing.T) {
	globals.JSON = true
	globals.Quiet = false
	defer func() { globals.JSON = false }()
	got, err := captureRPCOutput(t, func() error {
		return printLocalResponse("plugin.path", map[string]any{"path": "/tmp/plugin", "released": false})
	})
	if err != nil {
		t.Fatalf("print failed: %v", err)
	}
	var resp struct {
		Success bool `json:"success"`
		Meta    struct {
			Command string `json:"command"`
		} `json:"meta"`
	}
	if err := json.Unmarshal([]byte(got), &resp); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if !resp.Success || resp.Meta.Command != "plugin.path" {
		t.Fatalf("unexpected response: %s", got)
	}
}

func TestPrintLocalResponsePluginQuietPath(t *testing.T) {
	globals.JSON = false
	globals.Quiet = true
	defer func() { globals.Quiet = false }()
	got, err := captureRPCOutput(t, func() error {
		return printLocalResponse("plugin.path", map[string]any{"path": "/tmp/plugin", "released": false})
	})
	if err != nil {
		t.Fatalf("print failed: %v", err)
	}
	if got != "/tmp/plugin" {
		t.Fatalf("unexpected quiet output: %q", got)
	}
}
