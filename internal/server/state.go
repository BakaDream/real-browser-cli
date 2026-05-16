package server

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/bakadream/real-browser-cli/internal/protocol"
)

const (
	host                      = "127.0.0.1"
	apiPort                   = "18767"
	wsPort                    = "18765"
	idleShutdownTTL           = 300 * time.Second
	idleShutdownCheckInterval = 5 * time.Second
)

type AppState struct {
	Driver       *protocol.DriverState
	StartedAt    time.Time
	LastActivity time.Time
	Token        string
	Refs         map[string]ElementRef
	Console      []ConsoleEntry
	Errors       []ConsoleEntry
	Network      []NetworkEntry
	Trace        []TraceStep
	Dialog       *DialogState
	DNRRules     map[string]int
	NextDNRRule  int

	lastActivityMu sync.Mutex
	dataMu         sync.Mutex
	shutdownOnce   sync.Once
	Shutdown       chan struct{}
	SessionsReady  chan struct{}
}

type ElementRef struct {
	Ref           string         `json:"ref"`
	TabID         string         `json:"tabId"`
	ChromeTabID   int64          `json:"chromeTabId,omitempty"`
	SnapshotID    string         `json:"snapshotId"`
	BackendNodeID int64          `json:"backendNodeId,omitempty"`
	Role          string         `json:"role,omitempty"`
	Name          string         `json:"name,omitempty"`
	Value         string         `json:"value,omitempty"`
	Required      bool           `json:"required,omitempty"`
	Disabled      bool           `json:"disabled,omitempty"`
	Checked       bool           `json:"checked,omitempty"`
	Selected      bool           `json:"selected,omitempty"`
	Box           *Rect          `json:"box,omitempty"`
	Locators      map[string]any `json:"locators,omitempty"`
}

type Rect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type ConsoleEntry struct {
	TabID       string          `json:"tabId,omitempty"`
	ChromeTabID int64           `json:"chromeTabId,omitempty"`
	Level       string          `json:"level,omitempty"`
	Text        string          `json:"text,omitempty"`
	URL         string          `json:"url,omitempty"`
	Line        int             `json:"line,omitempty"`
	Column      int             `json:"column,omitempty"`
	Raw         json.RawMessage `json:"raw,omitempty"`
	Time        time.Time       `json:"time"`
}

type NetworkEntry struct {
	RequestID       string          `json:"requestId"`
	TabID           string          `json:"tabId,omitempty"`
	ChromeTabID     int64           `json:"chromeTabId,omitempty"`
	URL             string          `json:"url,omitempty"`
	Method          string          `json:"method,omitempty"`
	Status          int             `json:"status,omitempty"`
	Type            string          `json:"type,omitempty"`
	MimeType        string          `json:"mimeType,omitempty"`
	RequestHeaders  map[string]any  `json:"requestHeaders,omitempty"`
	ResponseHeaders map[string]any  `json:"responseHeaders,omitempty"`
	Body            string          `json:"body,omitempty"`
	Base64Encoded   bool            `json:"base64Encoded,omitempty"`
	BodyAvailable   bool            `json:"bodyAvailable,omitempty"`
	Pending         bool            `json:"pending,omitempty"`
	Failed          bool            `json:"failed,omitempty"`
	ErrorText       string          `json:"errorText,omitempty"`
	StartedAt       time.Time       `json:"startedAt,omitempty"`
	FinishedAt      time.Time       `json:"finishedAt,omitempty"`
	Raw             json.RawMessage `json:"raw,omitempty"`
	Time            time.Time       `json:"time"`
}

type DialogState struct {
	Open        bool            `json:"open"`
	TabID       string          `json:"tabId,omitempty"`
	ChromeTabID int64           `json:"chromeTabId,omitempty"`
	Type        string          `json:"type,omitempty"`
	Message     string          `json:"message,omitempty"`
	DefaultText string          `json:"defaultText,omitempty"`
	Raw         json.RawMessage `json:"raw,omitempty"`
	Time        time.Time       `json:"time,omitempty"`
}

type TraceStep struct {
	ID            string         `json:"id"`
	Time          time.Time      `json:"time"`
	Command       string         `json:"command"`
	TabID         string         `json:"tabId,omitempty"`
	URLBefore     string         `json:"urlBefore,omitempty"`
	URLAfter      string         `json:"urlAfter,omitempty"`
	Target        string         `json:"target,omitempty"`
	Ref           string         `json:"ref,omitempty"`
	Role          string         `json:"role,omitempty"`
	Name          string         `json:"name,omitempty"`
	ValueRedacted bool           `json:"valueRedacted,omitempty"`
	ValueLength   int            `json:"valueLength,omitempty"`
	Result        any            `json:"result,omitempty"`
	Error         string         `json:"error,omitempty"`
	DurationMS    int64          `json:"durationMs"`
	Params        map[string]any `json:"params,omitempty"`
}

func NewAppState(token string) *AppState {
	now := time.Now()
	return &AppState{
		Driver:        protocol.NewDriverState(),
		StartedAt:     now,
		LastActivity:  now,
		Token:         token,
		Refs:          make(map[string]ElementRef),
		DNRRules:      make(map[string]int),
		NextDNRRule:   20000,
		Shutdown:      make(chan struct{}),
		SessionsReady: make(chan struct{}, 1),
	}
}

func (s *AppState) Touch() {
	s.lastActivityMu.Lock()
	s.LastActivity = time.Now()
	s.lastActivityMu.Unlock()
}

func (s *AppState) IdleFor() time.Duration {
	s.lastActivityMu.Lock()
	defer s.lastActivityMu.Unlock()
	return time.Since(s.LastActivity)
}

func (s *AppState) RequestShutdown() {
	s.shutdownOnce.Do(func() {
		close(s.Shutdown)
	})
}

func (s *AppState) NotifySessionsReady() {
	select {
	case s.SessionsReady <- struct{}{}:
	default:
	}
}
