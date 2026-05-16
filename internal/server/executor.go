package server

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/bakadream/real-browser-cli/internal/htmlopt"
	"github.com/bakadream/real-browser-cli/internal/protocol"
	"github.com/google/uuid"
)

func ExecutePageJS(state *AppState, script string, switchTabID string, noMonitor bool) (map[string]any, error) {
	if err := SetDefaultSession(state, switchTabID); err != nil {
		return nil, err
	}
	var before string
	if !noMonitor {
		if html, err := GetHTML(state, false, 9_999_999, false); err == nil {
			before = html
		}
	}
	beforeTabs := make(map[string]bool)
	for _, tab := range ActiveTabs(state, true) {
		beforeTabs[tab.ID] = true
	}
	response, err := ExecuteRawJS(state, script, 15*time.Second)
	if err != nil {
		return nil, err
	}

	state.Driver.Mu.RLock()
	tabID := state.Driver.DefaultSessionID
	state.Driver.Mu.RUnlock()

	result := map[string]any{
		"value": execReturnValue(response),
		"tabId": tabID,
	}
	if len(response.NewTabs) > 0 {
		result["newTabs"] = json.RawMessage(response.NewTabs)
	} else {
		newTabs := make([]map[string]string, 0)
		for _, tab := range ActiveTabs(state, false) {
			if !beforeTabs[tab.ID] {
				newTabs = append(newTabs, map[string]string{"id": tab.ID, "url": tab.URL})
			}
		}
		if len(newTabs) > 0 {
			result["newTabs"] = newTabs
		}
	}
	if !noMonitor && before != "" {
		if currentHTML, err := GetHTML(state, false, 9_999_999, false); err == nil {
			result["change"] = json.RawMessage(htmlopt.ChangedElements(before, currentHTML))
		}
	}
	return result, nil
}

func ExecuteOpenCommand(state *AppState, payload string, switchTabID string) (map[string]any, error) {
	if err := SetDefaultSession(state, switchTabID); err != nil {
		return nil, err
	}
	response, err := executeRawWithAnySender(state, payload, 15*time.Second)
	if err != nil {
		return nil, err
	}
	state.Driver.Mu.RLock()
	tabID := state.Driver.DefaultSessionID
	state.Driver.Mu.RUnlock()
	result := map[string]any{
		"value": execReturnValue(response),
		"tabId": tabID,
	}
	if len(response.NewTabs) > 0 {
		result["newTabs"] = json.RawMessage(response.NewTabs)
	}
	return result, nil
}

func ExecuteRawJS(state *AppState, code string, timeout time.Duration) (protocol.ExecResult, error) {
	return ExecuteRawJSCommand(state, code, timeout, "js")
}

func ExecuteRawJSCommand(state *AppState, code string, timeout time.Duration, command string) (protocol.ExecResult, error) {
	sessionID, handle, session, err := ResolveSession(state, "")
	if err != nil {
		return protocol.ExecResult{}, fmt.Errorf("没有可用的浏览器标签页，查L3记忆分析原因。")
	}
	if err := RequireScriptable(session, handle); err != nil {
		return protocol.ExecResult{}, err
	}
	sender := session.Sender
	return executeRaw(state, code, sessionID, sender, timeout, command)
}

func executeRawWithAnySender(state *AppState, code string, timeout time.Duration) (protocol.ExecResult, error) {
	return executeRawWithAnySenderCommand(state, code, timeout, "ext")
}

func executeRawWithAnySenderCommand(state *AppState, code string, timeout time.Duration, command string) (protocol.ExecResult, error) {
	WaitForSessions(state, 5*time.Second)
	state.Driver.Mu.RLock()
	sessionID := state.Driver.DefaultSessionID
	var sender chan string
	if sessionID != "" {
		if session := state.Driver.Sessions[sessionID]; session.IsActive() {
			sender = session.Sender
		}
	}
	if sender == nil {
		for id, session := range state.Driver.Sessions {
			if session.IsActive() && session.Sender != nil {
				sessionID = id
				sender = session.Sender
				break
			}
		}
	}
	if sender == nil {
		for _, extSender := range state.Driver.ExtensionSenders {
			if extSender != nil {
				sender = extSender
				break
			}
		}
	}
	state.Driver.Mu.RUnlock()
	if sender == nil {
		return protocol.ExecResult{}, fmt.Errorf("扩展未连接")
	}
	return executeRaw(state, code, sessionID, sender, timeout, command)
}

func executeRaw(state *AppState, code string, sessionID string, sender chan string, timeout time.Duration, command string) (protocol.ExecResult, error) {
	execID := uuid.NewString()
	payload, _ := json.Marshal(map[string]any{
		"id":    execID,
		"code":  code,
		"tabId": parseTabID(sessionID),
	})
	pending := &protocol.PendingExec{Ch: make(chan protocol.ExecOutcome, 1)}
	state.Driver.Mu.Lock()
	state.Driver.Pending[execID] = pending
	state.Driver.ActiveExecSessions[execID] = sessionID
	state.Driver.ActiveExecCommands[execID] = command
	state.Driver.Mu.Unlock()

	select {
	case sender <- string(payload):
	default:
		cleanupPending(state, execID)
		return protocol.ExecResult{}, fmt.Errorf("浏览器扩展连接已断开")
	}

	select {
	case outcome := <-pending.Ch:
		if outcome.Err != nil {
			return protocol.ExecResult{}, outcome.Err
		}
		return outcome.Result, nil
	case <-time.After(timeout):
		state.Driver.Mu.Lock()
		acked := state.Driver.Acked[execID]
		delete(state.Driver.Acked, execID)
		delete(state.Driver.Pending, execID)
		delete(state.Driver.ActiveExecSessions, execID)
		delete(state.Driver.ActiveExecCommands, execID)
		state.Driver.Mu.Unlock()
		msg := fmt.Sprintf("No response data in %ds (no ACK, script may not have been delivered)", int(timeout.Seconds()))
		if acked {
			msg = fmt.Sprintf("No response data in %ds (ACK received, script may still be running)", int(timeout.Seconds()))
		}
		data, _ := json.Marshal(msg)
		return protocol.ExecResult{Result: data}, nil
	}
}

func cleanupPending(state *AppState, execID string) {
	state.Driver.Mu.Lock()
	delete(state.Driver.Pending, execID)
	delete(state.Driver.Acked, execID)
	delete(state.Driver.ActiveExecSessions, execID)
	delete(state.Driver.ActiveExecCommands, execID)
	state.Driver.Mu.Unlock()
}

func execReturnValue(result protocol.ExecResult) any {
	if len(result.Data) > 0 {
		return json.RawMessage(result.Data)
	}
	if len(result.Result) > 0 {
		return json.RawMessage(result.Result)
	}
	return nil
}

func parseTabID(id string) any {
	var n int64
	if _, err := fmt.Sscanf(id, "%d", &n); err == nil {
		return n
	}
	return 0
}
