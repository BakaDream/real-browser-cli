package protocol

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

type TabInfo struct {
	ID          string  `json:"id"`
	TabID       string  `json:"tabId,omitempty"`
	ChromeTabID int64   `json:"chromeTabId,omitempty"`
	URL         string  `json:"url"`
	Title       string  `json:"title"`
	Type        string  `json:"type"`
	Scriptable  bool    `json:"scriptable"`
	ConnectedAt float64 `json:"connected_at,omitempty"`
	Active      bool    `json:"active,omitempty"`
	WindowID    int64   `json:"windowId,omitempty"`
	Incognito   bool    `json:"incognito,omitempty"`
	Label       string  `json:"label,omitempty"`
	Status      string  `json:"status,omitempty"`
}

type Session struct {
	Info           TabInfo
	Sender         chan string
	DisconnectedAt *time.Time
}

func (s *Session) IsActive() bool {
	return s != nil && s.DisconnectedAt == nil
}

type ExecResult struct {
	Data    json.RawMessage `json:"data,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Closed  *uint8          `json:"closed,omitempty"`
	NewTabs json.RawMessage `json:"newTabs,omitempty"`
}

type ExecOutcome struct {
	Result ExecResult
	Err    error
}

type PendingExec struct {
	DeliveredAt *time.Time
	Ch          chan ExecOutcome
}

type DriverState struct {
	Sessions           map[string]*Session
	ExtensionSenders   map[string]chan string
	Pending            map[string]*PendingExec
	DefaultSessionID   string
	LatestSessionID    string
	ActiveExecSessions map[string]string
	ActiveExecCommands map[string]string
	Acked              map[string]bool
	TabHandles         map[string]string
	HandleToSession    map[string]string
	TabLabels          map[string]string
	ActiveHandle       string
	NextTabHandle      int
	Capabilities       map[string]bool
	Mu                 sync.RWMutex
}

func NewDriverState() *DriverState {
	return &DriverState{
		Sessions:           make(map[string]*Session),
		ExtensionSenders:   make(map[string]chan string),
		Pending:            make(map[string]*PendingExec),
		ActiveExecSessions: make(map[string]string),
		ActiveExecCommands: make(map[string]string),
		Acked:              make(map[string]bool),
		TabHandles:         make(map[string]string),
		HandleToSession:    make(map[string]string),
		TabLabels:          make(map[string]string),
		NextTabHandle:      1,
		Capabilities:       make(map[string]bool),
	}
}

type WsIncoming struct {
	Type             string          `json:"type"`
	ExtensionVersion string          `json:"extensionVersion,omitempty"`
	Capabilities     map[string]bool `json:"capabilities,omitempty"`
	Permissions      []string        `json:"permissions,omitempty"`
	Tabs             []ExtTab        `json:"tabs,omitempty"`
	Event            string          `json:"event,omitempty"`
	ChromeTabID      int64           `json:"chromeTabId,omitempty"`
	Payload          json.RawMessage `json:"payload,omitempty"`
	Time             float64         `json:"time,omitempty"`
	ID               string          `json:"id,omitempty"`
	OK               *bool           `json:"ok,omitempty"`
	Result           json.RawMessage `json:"result,omitempty"`
	Data             json.RawMessage `json:"data,omitempty"`
	Error            json.RawMessage `json:"error,omitempty"`
	Code             string          `json:"code,omitempty"`
	NewTabs          json.RawMessage `json:"newTabs,omitempty"`
}

type ExtTab struct {
	ID          any    `json:"id"`
	ChromeTabID any    `json:"chromeTabId,omitempty"`
	URL         string `json:"url"`
	Title       string `json:"title"`
	Scriptable  bool   `json:"scriptable,omitempty"`
	Active      bool   `json:"active,omitempty"`
	WindowID    int64  `json:"windowId,omitempty"`
	Incognito   bool   `json:"incognito,omitempty"`
}

func (t ExtTab) IntoTabInfo() TabInfo {
	id := ""
	rawID := t.ID
	if rawID == nil {
		rawID = t.ChromeTabID
	}
	switch v := rawID.(type) {
	case string:
		id = v
	case float64:
		id = fmt.Sprintf("%.0f", v)
	case json.Number:
		id = v.String()
	default:
		id = fmt.Sprintf("%v", v)
	}
	var chromeTabID int64
	fmt.Sscanf(id, "%d", &chromeTabID)
	return TabInfo{
		ID:          id,
		ChromeTabID: chromeTabID,
		URL:         t.URL,
		Title:       t.Title,
		Type:        "ext_ws",
		Scriptable:  t.Scriptable || scriptableURL(t.URL),
		Active:      t.Active,
		WindowID:    t.WindowID,
		Incognito:   t.Incognito,
	}
}

func scriptableURL(url string) bool {
	return strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "file://") || strings.HasPrefix(url, "data:")
}

type APIRequest struct {
	Version string          `json:"version,omitempty"`
	ID      string          `json:"id,omitempty"`
	Command string          `json:"command"`
	Target  map[string]any  `json:"target,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Options RequestOptions  `json:"options,omitempty"`
}

type RequestOptions struct {
	TimeoutMS int64 `json:"timeoutMs,omitempty"`
	JSON      bool  `json:"json,omitempty"`
	DebugIDs  bool  `json:"debugIds,omitempty"`
}

type APIResponse struct {
	ID      string         `json:"id,omitempty"`
	Success bool           `json:"success"`
	Data    any            `json:"data,omitempty"`
	Error   *APIError      `json:"error,omitempty"`
	Meta    map[string]any `json:"meta,omitempty"`
}

type APIError struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Retryable bool           `json:"retryable"`
	Details   map[string]any `json:"details"`
}
