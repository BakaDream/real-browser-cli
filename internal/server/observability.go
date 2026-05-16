package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/bakadream/real-browser-cli/internal/protocol"
	"github.com/google/uuid"
)

const (
	consoleLimit = 500
	errorsLimit  = 200
	networkLimit = 1000
	traceLimit   = 500
)

func handleBrowserEvent(state *AppState, incoming protocol.WsIncoming) {
	now := eventTime(incoming.Time)
	tabID := handleForChromeTab(state, incoming.ChromeTabID)
	switch incoming.Event {
	case "console.message":
		var p struct {
			Level  string `json:"level"`
			Text   string `json:"text"`
			URL    string `json:"url"`
			Line   int    `json:"line"`
			Column int    `json:"column"`
		}
		_ = json.Unmarshal(incoming.Payload, &p)
		appendConsole(state, ConsoleEntry{TabID: tabID, ChromeTabID: incoming.ChromeTabID, Level: p.Level, Text: p.Text, URL: p.URL, Line: p.Line, Column: p.Column, Raw: incoming.Payload, Time: now})
	case "runtime.exception":
		var p struct {
			Message string `json:"message"`
			Stack   string `json:"stack"`
			URL     string `json:"url"`
			Line    int    `json:"line"`
			Column  int    `json:"column"`
		}
		_ = json.Unmarshal(incoming.Payload, &p)
		text := p.Message
		if p.Stack != "" {
			text = text + "\n" + p.Stack
		}
		appendError(state, ConsoleEntry{TabID: tabID, ChromeTabID: incoming.ChromeTabID, Level: "error", Text: strings.TrimSpace(text), URL: p.URL, Line: p.Line, Column: p.Column, Raw: incoming.Payload, Time: now})
	case "network.request":
		mergeNetwork(state, incoming.ChromeTabID, tabID, incoming.Payload, now, func(entry *NetworkEntry, p map[string]any) {
			entry.Pending = true
			entry.StartedAt = now
			entry.Time = now
			entry.Type = stringFromMap(p, "type")
			if req, ok := p["request"].(map[string]any); ok {
				entry.URL = stringFromMap(req, "url")
				entry.Method = stringFromMap(req, "method")
				entry.RequestHeaders = mapFromAny(req["headers"])
			}
		})
	case "network.response":
		mergeNetwork(state, incoming.ChromeTabID, tabID, incoming.Payload, now, func(entry *NetworkEntry, p map[string]any) {
			entry.Type = firstNonEmptyString(entry.Type, stringFromMap(p, "type"))
			if resp, ok := p["response"].(map[string]any); ok {
				entry.URL = firstNonEmptyString(entry.URL, stringFromMap(resp, "url"))
				entry.Status = intFromAny(resp["status"])
				entry.MimeType = stringFromMap(resp, "mimeType")
				entry.ResponseHeaders = mapFromAny(resp["headers"])
				entry.BodyAvailable = true
			}
			entry.Time = now
		})
	case "network.finished":
		mergeNetwork(state, incoming.ChromeTabID, tabID, incoming.Payload, now, func(entry *NetworkEntry, p map[string]any) {
			entry.Pending = false
			entry.FinishedAt = now
			entry.BodyAvailable = true
			entry.Time = now
		})
	case "network.failed":
		mergeNetwork(state, incoming.ChromeTabID, tabID, incoming.Payload, now, func(entry *NetworkEntry, p map[string]any) {
			entry.Pending = false
			entry.Failed = true
			entry.ErrorText = stringFromMap(p, "errorText")
			entry.FinishedAt = now
			entry.Time = now
		})
	case "dialog.opened":
		var p struct {
			Type        string `json:"type"`
			Message     string `json:"message"`
			DefaultText string `json:"defaultText"`
		}
		_ = json.Unmarshal(incoming.Payload, &p)
		state.dataMu.Lock()
		state.Dialog = &DialogState{Open: true, TabID: tabID, ChromeTabID: incoming.ChromeTabID, Type: p.Type, Message: p.Message, DefaultText: p.DefaultText, Raw: incoming.Payload, Time: now}
		state.dataMu.Unlock()
		resolvePendingDialogOpen(state, incoming.ChromeTabID, tabID, p.Type, p.Message, p.DefaultText)
	case "dialog.closed":
		state.dataMu.Lock()
		state.Dialog = nil
		state.dataMu.Unlock()
	}
}

func resolvePendingDialogOpen(state *AppState, chromeTabID int64, tabID string, dialogType string, message string, defaultText string) {
	result, _ := json.Marshal(map[string]any{
		"dialogOpened": true,
		"type":         dialogType,
		"message":      message,
		"defaultText":  defaultText,
		"tabId":        tabID,
		"chromeTabId":  chromeTabID,
	})
	type pendingResolution struct {
		pending *protocol.PendingExec
	}
	resolutions := make([]pendingResolution, 0)
	state.Driver.Mu.Lock()
	for execID, sessionID := range state.Driver.ActiveExecSessions {
		session := state.Driver.Sessions[sessionID]
		if session == nil || session.Info.ChromeTabID != chromeTabID {
			continue
		}
		command := state.Driver.ActiveExecCommands[execID]
		if !isDialogCompletableCommand(command) {
			continue
		}
		pending := state.Driver.Pending[execID]
		if pending == nil {
			continue
		}
		resolutions = append(resolutions, pendingResolution{pending: pending})
		delete(state.Driver.Pending, execID)
		delete(state.Driver.Acked, execID)
		delete(state.Driver.ActiveExecSessions, execID)
		delete(state.Driver.ActiveExecCommands, execID)
	}
	state.Driver.Mu.Unlock()
	for _, item := range resolutions {
		select {
		case item.pending.Ch <- protocol.ExecOutcome{Result: protocol.ExecResult{Data: result}}:
		default:
		}
	}
}

func isDialogCompletableCommand(command string) bool {
	if command == "" {
		return false
	}
	if command == "eval" || command == "js" {
		return true
	}
	if strings.HasPrefix(command, "action.click") || strings.HasPrefix(command, "action.dblclick") {
		return true
	}
	if command == "cdp.Input.dispatchMouseEvent" || command == "cdp.Runtime.evaluate" || strings.HasPrefix(command, "cdp.Page.") {
		return true
	}
	return false
}

func appendConsole(state *AppState, entry ConsoleEntry) {
	state.dataMu.Lock()
	defer state.dataMu.Unlock()
	state.Console = append(state.Console, entry)
	if len(state.Console) > consoleLimit {
		state.Console = state.Console[len(state.Console)-consoleLimit:]
	}
}

func appendError(state *AppState, entry ConsoleEntry) {
	state.dataMu.Lock()
	defer state.dataMu.Unlock()
	state.Errors = append(state.Errors, entry)
	if len(state.Errors) > errorsLimit {
		state.Errors = state.Errors[len(state.Errors)-errorsLimit:]
	}
}

func mergeNetwork(state *AppState, chromeTabID int64, tabID string, raw json.RawMessage, now time.Time, apply func(*NetworkEntry, map[string]any)) {
	var p map[string]any
	_ = json.Unmarshal(raw, &p)
	requestID := stringFromMap(p, "requestId")
	if requestID == "" {
		return
	}
	state.dataMu.Lock()
	defer state.dataMu.Unlock()
	index := -1
	for i := range state.Network {
		if state.Network[i].RequestID == requestID {
			index = i
			break
		}
	}
	if index < 0 {
		state.Network = append(state.Network, NetworkEntry{RequestID: requestID, TabID: tabID, ChromeTabID: chromeTabID, Raw: raw, Time: now})
		if len(state.Network) > networkLimit {
			state.Network = state.Network[len(state.Network)-networkLimit:]
		}
		index = len(state.Network) - 1
	}
	entry := &state.Network[index]
	entry.Raw = raw
	if entry.TabID == "" {
		entry.TabID = tabID
	}
	if entry.ChromeTabID == 0 {
		entry.ChromeTabID = chromeTabID
	}
	apply(entry, p)
}

func appendTrace(state *AppState, step TraceStep) {
	state.dataMu.Lock()
	defer state.dataMu.Unlock()
	state.Trace = append(state.Trace, step)
	if len(state.Trace) > traceLimit {
		state.Trace = state.Trace[len(state.Trace)-traceLimit:]
	}
}

func shouldTraceCommand(command string) bool {
	if command == "" {
		return false
	}
	if strings.HasPrefix(command, "trace.") || strings.HasPrefix(command, "export.") {
		return false
	}
	if strings.HasPrefix(command, "console.") || strings.HasPrefix(command, "errors.") || strings.HasPrefix(command, "network.") {
		return false
	}
	switch command {
	case "doctor", "tab.list", "tab.active", "dialog.status":
		return false
	default:
		return true
	}
}

func buildTraceStep(state *AppState, req protocol.APIRequest, params rpcParams, tabID, beforeURL, afterURL string, result any, err error, duration time.Duration) TraceStep {
	step := TraceStep{
		ID:         "tr_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12],
		Time:       time.Now(),
		Command:    req.Command,
		TabID:      tabID,
		URLBefore:  beforeURL,
		URLAfter:   afterURL,
		Target:     firstNonEmptyString(stringParam(params, "target"), stringParam(params, "tab")),
		DurationMS: duration.Milliseconds(),
	}
	if strings.HasPrefix(step.Target, "@") {
		step.Ref = strings.TrimPrefix(step.Target, "@")
		if ref, ok := resolveElementRef(state, step.Target); ok {
			step.Role = ref.Role
			step.Name = ref.Name
		}
	}
	if req.Command == "action.fill" || req.Command == "action.type" {
		value := stringParam(params, "value")
		step.ValueRedacted = true
		step.ValueLength = len([]rune(value))
	}
	if req.Command == "action.press" {
		step.Params = map[string]any{"value": stringParam(params, "value")}
	}
	if req.Command == "open" && stringParam(params, "url") != "" {
		step.URLAfter = NormalizeURL(stringParam(params, "url"))
	}
	if req.Command == "tab.new" && stringParam(params, "url") != "" {
		step.URLAfter = NormalizeURL(stringParam(params, "url"))
	}
	if req.Command == "eval" {
		step.Params = map[string]any{"scriptLength": len(stringParam(params, "script"))}
	}
	if req.Command == "cdp" {
		step.Params = map[string]any{"method": stringParam(params, "method")}
	}
	if err != nil {
		step.Error = err.Error()
	} else {
		step.Result = compactTraceResult(result)
	}
	return step
}

func currentTraceContext(state *AppState, target string) (string, string) {
	state.Driver.Mu.RLock()
	defer state.Driver.Mu.RUnlock()
	sessionID := ""
	if target != "" {
		if resolved, ok := resolveSessionLocked(state.Driver, target); ok {
			sessionID = resolved
		}
	}
	if sessionID == "" {
		sessionID = state.Driver.DefaultSessionID
	}
	if session := state.Driver.Sessions[sessionID]; session.IsActive() {
		return state.Driver.TabHandles[sessionID], session.Info.URL
	}
	return state.Driver.ActiveHandle, ""
}

func compactTraceResult(result any) any {
	switch v := result.(type) {
	case string:
		if len(v) > 200 {
			return v[:200] + "..."
		}
		return v
	case map[string]any:
		out := map[string]any{}
		for _, key := range []string{"tabId", "snapshotId", "path", "matched"} {
			if value, ok := v[key]; ok {
				out[key] = value
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

func handleForChromeTab(state *AppState, chromeTabID int64) string {
	if chromeTabID == 0 {
		return ""
	}
	sessionID := fmt.Sprintf("%d", chromeTabID)
	state.Driver.Mu.RLock()
	defer state.Driver.Mu.RUnlock()
	return state.Driver.TabHandles[sessionID]
}

func eventTime(seconds float64) time.Time {
	if seconds <= 0 {
		return time.Now()
	}
	if seconds > 1_000_000_000_000 {
		return time.UnixMilli(int64(seconds))
	}
	return time.Unix(0, int64(seconds*float64(time.Second)))
}

func stringFromMap(m map[string]any, key string) string {
	value, _ := m[key].(string)
	return value
}

func intFromAny(value any) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	default:
		return 0
	}
}

func mapFromAny(value any) map[string]any {
	if value == nil {
		return nil
	}
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return nil
}

func mapValue(value any, key string) any {
	if m, ok := value.(map[string]any); ok {
		return m[key]
	}
	return nil
}

func intFromNested(value any, path ...string) int {
	current := value
	for _, key := range path {
		current = mapValue(current, key)
	}
	return intFromAny(current)
}

func networkPending(state *AppState, tab string) int {
	state.dataMu.Lock()
	defer state.dataMu.Unlock()
	count := 0
	for _, item := range state.Network {
		if !item.Pending {
			continue
		}
		if tab != "" && item.TabID != tab {
			continue
		}
		count++
	}
	return count
}
