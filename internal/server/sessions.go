package server

import (
	"fmt"
	"strconv"
	"time"

	"github.com/bakadream/real-browser-cli/internal/protocol"
)

func RegisterTabs(state *AppState, tabs []protocol.ExtTab, sender chan string, registeredIDs *[]string) {
	current := make(map[string]bool, len(tabs))
	for _, tab := range tabs {
		info := tab.IntoTabInfo()
		current[info.ID] = true
	}

	state.Driver.Mu.Lock()
	defer state.Driver.Mu.Unlock()

	now := time.Now()
	for _, session := range state.Driver.Sessions {
		if session.Info.Type == "ext_ws" && !current[session.Info.ID] {
			session.DisconnectedAt = &now
			if handle := state.Driver.TabHandles[session.Info.ID]; handle != "" && state.Driver.ActiveHandle == handle {
				state.Driver.ActiveHandle = ""
			}
		}
	}
	for _, tab := range tabs {
		info := tab.IntoTabInfo()
		if !containsString(*registeredIDs, info.ID) {
			*registeredIDs = append(*registeredIDs, info.ID)
		}
		handle := state.Driver.TabHandles[info.ID]
		if handle == "" {
			handle = fmt.Sprintf("t%d", state.Driver.NextTabHandle)
			state.Driver.NextTabHandle++
			state.Driver.TabHandles[info.ID] = handle
		}
		state.Driver.HandleToSession[handle] = info.ID
		info.TabID = handle
		info.Status = "active"
		if !info.Scriptable {
			info.Status = "unsupported"
		}
		if info.Active || state.Driver.ActiveHandle == "" {
			state.Driver.ActiveHandle = handle
		}
		state.Driver.LatestSessionID = info.ID
		if state.Driver.DefaultSessionID == "" {
			state.Driver.DefaultSessionID = info.ID
		}
		state.Driver.Sessions[info.ID] = &protocol.Session{
			Info:   info,
			Sender: sender,
		}
	}
	if state.Driver.DefaultSessionID == "" {
		state.Driver.DefaultSessionID = activeSessionLocked(state.Driver)
	}
	state.NotifySessionsReady()
}

func RegisterExtensionSender(state *AppState, id string, sender chan string) {
	state.Driver.Mu.Lock()
	state.Driver.ExtensionSenders[id] = sender
	state.Driver.Mu.Unlock()
	state.NotifySessionsReady()
}

func UnregisterExtensionSender(state *AppState, id string) {
	state.Driver.Mu.Lock()
	delete(state.Driver.ExtensionSenders, id)
	state.Driver.Mu.Unlock()
}

func MarkDisconnected(state *AppState, registeredIDs []string) {
	now := time.Now()
	state.Driver.Mu.Lock()
	defer state.Driver.Mu.Unlock()
	registered := make(map[string]bool, len(registeredIDs))
	for _, id := range registeredIDs {
		registered[id] = true
		if session, ok := state.Driver.Sessions[id]; ok {
			session.DisconnectedAt = &now
		}
	}
	for execID, sessionID := range state.Driver.ActiveExecSessions {
		if !registered[sessionID] {
			continue
		}
		if pending := state.Driver.Pending[execID]; pending != nil {
			select {
			case pending.Ch <- protocol.ExecOutcome{Err: fmt.Errorf("bridge_not_connected")}:
			default:
			}
		}
		delete(state.Driver.Pending, execID)
		delete(state.Driver.Acked, execID)
		delete(state.Driver.ActiveExecSessions, execID)
		delete(state.Driver.ActiveExecCommands, execID)
	}
	if state.Driver.DefaultSessionID != "" {
		if session := state.Driver.Sessions[state.Driver.DefaultSessionID]; !session.IsActive() {
			state.Driver.DefaultSessionID = activeSessionLocked(state.Driver)
		}
	}
	if state.Driver.ActiveHandle != "" {
		sessionID := state.Driver.HandleToSession[state.Driver.ActiveHandle]
		if session := state.Driver.Sessions[sessionID]; !session.IsActive() {
			state.Driver.ActiveHandle = activeHandleLocked(state.Driver)
		}
	}
}

func ActiveTabs(state *AppState, waitReady bool) []protocol.TabInfo {
	if waitReady {
		WaitForSessions(state, 5*time.Second)
	}
	state.Driver.Mu.RLock()
	defer state.Driver.Mu.RUnlock()
	tabs := make([]protocol.TabInfo, 0, len(state.Driver.Sessions))
	for _, session := range state.Driver.Sessions {
		if !session.IsActive() {
			continue
		}
		info := session.Info
		if handle := state.Driver.TabHandles[info.ID]; handle != "" {
			info.TabID = handle
			info.ID = handle
		}
		info.Status = "active"
		if !info.Scriptable {
			info.Status = "unsupported"
		}
		for label, handle := range state.Driver.TabLabels {
			if handle == info.TabID {
				info.Label = label
				break
			}
		}
		if len(info.URL) > 50 {
			info.URL = takeRunes(info.URL, 50) + "..."
		}
		tabs = append(tabs, info)
	}
	return tabs
}

func WaitForSessions(state *AppState, timeout time.Duration) bool {
	if HasActiveSessions(state) {
		return true
	}
	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return HasActiveSessions(state)
		}
		wait := remaining
		if wait > 200*time.Millisecond {
			wait = 200 * time.Millisecond
		}
		select {
		case <-state.SessionsReady:
			if HasActiveSessions(state) {
				return true
			}
		case <-time.After(wait):
			if HasActiveSessions(state) {
				return true
			}
		}
	}
}

func HasActiveSessions(state *AppState) bool {
	state.Driver.Mu.RLock()
	defer state.Driver.Mu.RUnlock()
	for _, session := range state.Driver.Sessions {
		if session.IsActive() {
			return true
		}
	}
	return false
}

func HasExtensionSender(state *AppState) bool {
	state.Driver.Mu.RLock()
	defer state.Driver.Mu.RUnlock()
	return len(state.Driver.ExtensionSenders) > 0
}

func SetDefaultSession(state *AppState, tabID string) error {
	state.Driver.Mu.Lock()
	defer state.Driver.Mu.Unlock()
	if tabID != "" {
		if sessionID, ok := resolveSessionLocked(state.Driver, tabID); ok {
			if session := state.Driver.Sessions[sessionID]; !session.IsActive() {
				return fmt.Errorf("tab not connected: %s", tabID)
			}
			state.Driver.DefaultSessionID = sessionID
			if handle := state.Driver.TabHandles[sessionID]; handle != "" {
				state.Driver.ActiveHandle = handle
			}
			return nil
		}
		return fmt.Errorf("tab not found: %s", tabID)
	}
	if session := state.Driver.Sessions[state.Driver.DefaultSessionID]; session.IsActive() {
		return nil
	}
	state.Driver.DefaultSessionID = activeSessionLocked(state.Driver)
	return nil
}

func ResolveSession(state *AppState, target string) (string, string, *protocol.Session, error) {
	WaitForSessions(state, 5*time.Second)
	state.Driver.Mu.Lock()
	defer state.Driver.Mu.Unlock()
	if target != "" {
		sessionID, ok := resolveSessionLocked(state.Driver, target)
		if !ok {
			return "", "", nil, fmt.Errorf("tab not found: %s", target)
		}
		session := state.Driver.Sessions[sessionID]
		if !session.IsActive() {
			return "", "", nil, fmt.Errorf("tab not connected: %s", target)
		}
		handle := state.Driver.TabHandles[sessionID]
		state.Driver.DefaultSessionID = sessionID
		state.Driver.ActiveHandle = handle
		return sessionID, handle, session, nil
	}
	sessionID := state.Driver.DefaultSessionID
	if session := state.Driver.Sessions[sessionID]; !session.IsActive() {
		sessionID = activeSessionLocked(state.Driver)
		state.Driver.DefaultSessionID = sessionID
	}
	if sessionID == "" {
		return "", "", nil, fmt.Errorf("tab_not_found")
	}
	session := state.Driver.Sessions[sessionID]
	if !session.IsActive() {
		return "", "", nil, fmt.Errorf("tab_not_found")
	}
	handle := state.Driver.TabHandles[sessionID]
	state.Driver.ActiveHandle = handle
	return sessionID, handle, session, nil
}

func RequireScriptable(session *protocol.Session, target string) error {
	if session == nil || !session.IsActive() {
		return fmt.Errorf("tab not connected: %s", target)
	}
	if !session.Info.Scriptable {
		if target == "" {
			target = session.Info.ID
		}
		return rpcCodeError{Code: "unsupported_tab", Message: "tab is not scriptable: " + target}
	}
	return nil
}

func SetTabLabel(state *AppState, target string, label string) (protocol.TabInfo, error) {
	state.Driver.Mu.Lock()
	defer state.Driver.Mu.Unlock()
	sessionID, ok := resolveSessionLocked(state.Driver, target)
	if !ok {
		return protocol.TabInfo{}, fmt.Errorf("tab not found: %s", target)
	}
	handle := state.Driver.TabHandles[sessionID]
	if existing := state.Driver.TabLabels[label]; existing != "" && existing != handle {
		return protocol.TabInfo{}, fmt.Errorf("label_conflict")
	}
	for name, current := range state.Driver.TabLabels {
		if current == handle {
			delete(state.Driver.TabLabels, name)
		}
	}
	state.Driver.TabLabels[label] = handle
	info := state.Driver.Sessions[sessionID].Info
	info.ID = handle
	info.TabID = handle
	info.Label = label
	info.Status = "active"
	if !info.Scriptable {
		info.Status = "unsupported"
	}
	return info, nil
}

func resolveSessionLocked(driver *protocol.DriverState, target string) (string, bool) {
	if target == "" {
		return "", false
	}
	if session := driver.Sessions[target]; session != nil {
		return target, true
	}
	if sessionID := driver.HandleToSession[target]; sessionID != "" {
		return sessionID, true
	}
	if handle := driver.TabLabels[target]; handle != "" {
		if sessionID := driver.HandleToSession[handle]; sessionID != "" {
			return sessionID, true
		}
	}
	if _, err := strconv.ParseInt(target, 10, 64); err == nil {
		if session := driver.Sessions[target]; session != nil {
			return target, true
		}
	}
	return "", false
}

func activeSessionLocked(driver *protocol.DriverState) string {
	if driver.ActiveHandle != "" {
		if sessionID := driver.HandleToSession[driver.ActiveHandle]; sessionID != "" {
			if session := driver.Sessions[sessionID]; session.IsActive() {
				return sessionID
			}
		}
	}
	var only string
	for id, session := range driver.Sessions {
		if !session.IsActive() {
			continue
		}
		if session.Info.Active {
			return id
		}
		if only != "" {
			only = ""
			continue
		}
		only = id
	}
	if only != "" {
		return only
	}
	for id, session := range driver.Sessions {
		if session.IsActive() {
			return id
		}
	}
	return ""
}

func activeHandleLocked(driver *protocol.DriverState) string {
	sessionID := activeSessionLocked(driver)
	if sessionID == "" {
		return ""
	}
	return driver.TabHandles[sessionID]
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
