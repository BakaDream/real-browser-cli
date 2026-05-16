package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/bakadream/real-browser-cli/internal/protocol"
)

func TestNormalizeURLPreservesExistingSchemes(t *testing.T) {
	cases := map[string]string{
		"about:blank":              "about:blank",
		"file:///tmp/a.txt":        "file:///tmp/a.txt",
		"chrome://extensions/":     "chrome://extensions/",
		"data:text/plain,hello":    "data:text/plain,hello",
		"https://example.com":      "https://example.com",
		"http://example.com":       "http://example.com",
		"example.com":              "https://example.com",
		"localhost:3000/no-scheme": "https://localhost:3000/no-scheme",
	}
	for input, want := range cases {
		if got := NormalizeURL(input); got != want {
			t.Fatalf("NormalizeURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSetDefaultSessionRejectsUnknownTab(t *testing.T) {
	state := NewAppState("token")
	now := time.Now()
	state.Driver.Sessions["101"] = &protocol.Session{
		Info: protocol.TabInfo{ID: "101", TabID: "t1", ChromeTabID: 101},
	}
	state.Driver.TabHandles["101"] = "t1"
	state.Driver.HandleToSession["t1"] = "101"
	state.Driver.DefaultSessionID = "101"
	state.Driver.ActiveHandle = "t1"
	state.Driver.Sessions["202"] = &protocol.Session{
		Info:           protocol.TabInfo{ID: "202", TabID: "t2", ChromeTabID: 202},
		DisconnectedAt: &now,
	}
	state.Driver.TabHandles["202"] = "t2"
	state.Driver.HandleToSession["t2"] = "202"

	if err := SetDefaultSession(state, "missing"); err == nil {
		t.Fatal("expected missing tab error")
	}
	if state.Driver.DefaultSessionID != "101" {
		t.Fatalf("default session changed after missing tab: %s", state.Driver.DefaultSessionID)
	}
	if err := SetDefaultSession(state, "t2"); err == nil {
		t.Fatal("expected disconnected tab error")
	}
	if state.Driver.DefaultSessionID != "101" {
		t.Fatalf("default session changed after disconnected tab: %s", state.Driver.DefaultSessionID)
	}
}

func TestWaitLoadScriptsAreDistinct(t *testing.T) {
	loadScript := waitLoadScript("load")
	if !strings.Contains(loadScript, "document.readyState === 'complete'") || strings.Contains(loadScript, "interactive") {
		t.Fatalf("load script should require complete only: %s", loadScript)
	}
	domScript := waitLoadScript("domcontentloaded")
	if !strings.Contains(domScript, "interactive") {
		t.Fatalf("domcontentloaded script should allow interactive: %s", domScript)
	}
}

func TestHandleFromOpenResultUsesReturnedChromeTabID(t *testing.T) {
	state := NewAppState("token")
	state.Driver.TabHandles["101"] = "t1"
	state.Driver.HandleToSession["t1"] = "101"
	state.Driver.TabHandles["202"] = "t2"
	state.Driver.HandleToSession["t2"] = "202"
	state.Driver.ActiveHandle = "t1"

	got := handleFromOpenResult(state, map[string]any{"chromeTabId": float64(202)})
	if got != "t2" {
		t.Fatalf("handleFromOpenResult = %q, want t2", got)
	}
}

func TestRPCErrorKeepsTargetNotFoundDistinctFromTabNotFound(t *testing.T) {
	target := rpcError("id", rpcCodeError{Code: "target_not_found", Message: "target not found: #missing"})
	if target.Error == nil || target.Error.Code != "target_not_found" {
		t.Fatalf("target error code = %#v", target.Error)
	}
	tab := rpcError("id", rpcCodeError{Code: "tab_not_found", Message: "tab not found: missing"})
	if tab.Error == nil || tab.Error.Code != "tab_not_found" {
		t.Fatalf("tab error code = %#v", tab.Error)
	}
}

func TestConsoleWarnAliasMatchesWarning(t *testing.T) {
	state := NewAppState("token")
	state.Console = []ConsoleEntry{{Level: "warning", Text: "warned"}, {Level: "log", Text: "logged"}}
	got := rpcConsoleList(state, "warn")
	items, _ := got["console"].([]ConsoleEntry)
	if len(items) != 1 || items[0].Text != "warned" {
		t.Fatalf("warn alias returned %#v", got["console"])
	}
}

func TestNetworkListFiltersExtensionRequestsByDefault(t *testing.T) {
	state := NewAppState("token")
	state.Network = []NetworkEntry{
		{RequestID: "page", URL: "https://example.com/"},
		{RequestID: "ext", URL: "chrome-extension://abc/script.js"},
	}
	got := rpcNetworkList(state, rpcParams{})
	items, _ := got["requests"].([]NetworkEntry)
	if len(items) != 1 || items[0].RequestID != "page" {
		t.Fatalf("default network list returned %#v", got["requests"])
	}
	got = rpcNetworkList(state, rpcParams{"includeExtension": true})
	items, _ = got["requests"].([]NetworkEntry)
	if len(items) != 2 {
		t.Fatalf("includeExtension network list returned %#v", got["requests"])
	}
}

func TestBatchItemAcceptsCLIStyleCommand(t *testing.T) {
	req, err := batchItemRequest(map[string]any{"cmd": "fill", "args": []any{"#name", "Alice"}})
	if err != nil {
		t.Fatalf("batch item failed: %v", err)
	}
	if req.Command != "action.fill" {
		t.Fatalf("command = %q", req.Command)
	}
	var params map[string]any
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatalf("params decode failed: %v", err)
	}
	if params["target"] != "#name" || params["value"] != "Alice" {
		t.Fatalf("params = %#v", params)
	}
}

func TestResolvePendingDialogOpenCompletesPendingExec(t *testing.T) {
	state := NewAppState("token")
	pending := &protocol.PendingExec{Ch: make(chan protocol.ExecOutcome, 1)}
	state.Driver.Sessions["101"] = &protocol.Session{Info: protocol.TabInfo{ID: "101", ChromeTabID: 101, Scriptable: true}}
	state.Driver.Pending["exec-1"] = pending
	state.Driver.Acked["exec-1"] = true
	state.Driver.ActiveExecSessions["exec-1"] = "101"
	state.Driver.ActiveExecCommands["exec-1"] = "cdp.Input.dispatchMouseEvent"

	resolvePendingDialogOpen(state, 101, "t1", "alert", "hello", "")

	select {
	case outcome := <-pending.Ch:
		if outcome.Err != nil {
			t.Fatalf("unexpected error: %v", outcome.Err)
		}
		var data map[string]any
		if err := json.Unmarshal(outcome.Result.Data, &data); err != nil {
			t.Fatalf("result decode failed: %v", err)
		}
		if data["dialogOpened"] != true || data["message"] != "hello" {
			t.Fatalf("unexpected dialog result: %#v", data)
		}
	case <-time.After(time.Second):
		t.Fatal("pending exec was not completed")
	}
	if state.Driver.Pending["exec-1"] != nil {
		t.Fatal("pending exec was not cleaned up")
	}
	if state.Driver.ActiveExecSessions["exec-1"] != "" {
		t.Fatal("active exec session was not cleaned up")
	}
}

func TestResolvePendingDialogOpenDoesNotCompleteUnrelatedExec(t *testing.T) {
	state := NewAppState("token")
	pending := &protocol.PendingExec{Ch: make(chan protocol.ExecOutcome, 1)}
	state.Driver.Sessions["101"] = &protocol.Session{Info: protocol.TabInfo{ID: "101", ChromeTabID: 101, Scriptable: true}}
	state.Driver.Pending["exec-1"] = pending
	state.Driver.ActiveExecSessions["exec-1"] = "101"
	state.Driver.ActiveExecCommands["exec-1"] = "network.get"

	resolvePendingDialogOpen(state, 101, "t1", "alert", "hello", "")

	select {
	case outcome := <-pending.Ch:
		t.Fatalf("unrelated pending exec was completed: %#v", outcome)
	default:
	}
	if state.Driver.Pending["exec-1"] == nil {
		t.Fatal("unrelated pending exec was cleaned up")
	}
}

func TestDialogCompletableCommandRejectsEmptyCommand(t *testing.T) {
	if isDialogCompletableCommand("") {
		t.Fatal("empty command should not be dialog-completable")
	}
	if !isDialogCompletableCommand("eval") || !isDialogCompletableCommand("cdp.Input.dispatchMouseEvent") {
		t.Fatal("expected eval and input dispatch to be dialog-completable")
	}
}

func TestDialogStatusReturnsCachedOpenDialogWithoutObserve(t *testing.T) {
	state := NewAppState("token")
	state.Dialog = &DialogState{Open: true, TabID: "t1", ChromeTabID: 101, Type: "prompt", Message: "hello"}
	resp, err := dispatchRPC(state, protocol.APIRequest{Command: "dialog.status"})
	if err != nil {
		t.Fatalf("dialog status failed: %v", err)
	}
	dialog, ok := resp.(*DialogState)
	if !ok {
		t.Fatalf("dialog status response = %#v", resp)
	}
	if !dialog.Open || dialog.Message != "hello" {
		t.Fatalf("dialog status returned %#v", dialog)
	}
}
