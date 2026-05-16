package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/bakadream/real-browser-cli/internal/protocol"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: allowedWSOrigin,
}

func NewWSHandler(state *AppState) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !allowedWSOrigin(r) || !secureTokenEqual(r.URL.Query().Get("token"), state.Token) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		go handleSocket(conn, state)
	})
	return mux
}

func allowedWSOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	if origin == "http://127.0.0.1" || origin == "http://localhost" {
		return true
	}
	return strings.HasPrefix(origin, "chrome-extension://")
}

func handleSocket(conn *websocket.Conn, state *AppState) {
	sender := make(chan string, 256)
	done := make(chan struct{})
	stopWriter := make(chan struct{})
	registeredIDs := make([]string, 0)
	connectionID := uuid.NewString()
	RegisterExtensionSender(state, connectionID, sender)

	go func() {
		defer close(done)
		for {
			select {
			case msg := <-sender:
				if err := conn.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
					return
				}
			case <-stopWriter:
				return
			case <-state.Shutdown:
				return
			}
		}
	}()

	defer func() {
		UnregisterExtensionSender(state, connectionID)
		MarkDisconnected(state, registeredIDs)
		close(stopWriter)
		_ = conn.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	}()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			return
		}
		text := string(message)
		if text == `{"type":"ping"}` {
			continue
		}
		var incoming protocol.WsIncoming
		if err := json.Unmarshal(message, &incoming); err != nil {
			fmt.Printf("WebSocket 消息解析失败: %v: %s\n", err, text)
			continue
		}
		handleWSMessage(incoming, state, sender, &registeredIDs)
	}
}

func handleWSMessage(incoming protocol.WsIncoming, state *AppState, sender chan string, registeredIDs *[]string) {
	switch incoming.Type {
	case "ready", "ext_ready", "tabs_update":
		if incoming.Capabilities != nil {
			state.Driver.Mu.Lock()
			state.Driver.Capabilities = incoming.Capabilities
			state.Driver.Mu.Unlock()
		}
		RegisterTabs(state, incoming.Tabs, sender, registeredIDs)
	case "event":
		handleBrowserEvent(state, incoming)
	case "ack":
		state.Driver.Mu.Lock()
		state.Driver.Acked[incoming.ID] = true
		if pending, ok := state.Driver.Pending[incoming.ID]; ok {
			now := time.Now()
			pending.DeliveredAt = &now
		}
		state.Driver.Mu.Unlock()
	case "result":
		data := incoming.Result
		if len(data) == 0 {
			data = incoming.Data
		}
		state.Driver.Mu.Lock()
		pending := state.Driver.Pending[incoming.ID]
		delete(state.Driver.Pending, incoming.ID)
		delete(state.Driver.Acked, incoming.ID)
		delete(state.Driver.ActiveExecSessions, incoming.ID)
		delete(state.Driver.ActiveExecCommands, incoming.ID)
		state.Driver.Mu.Unlock()
		if pending != nil {
			if incoming.OK != nil && !*incoming.OK {
				pending.Ch <- protocol.ExecOutcome{Err: fmt.Errorf("%s", incoming.Error)}
				return
			}
			pending.Ch <- protocol.ExecOutcome{Result: protocol.ExecResult{Data: data, NewTabs: incoming.NewTabs}}
		}
	case "error":
		state.Driver.Mu.Lock()
		pending := state.Driver.Pending[incoming.ID]
		delete(state.Driver.Pending, incoming.ID)
		delete(state.Driver.Acked, incoming.ID)
		delete(state.Driver.ActiveExecSessions, incoming.ID)
		delete(state.Driver.ActiveExecCommands, incoming.ID)
		state.Driver.Mu.Unlock()
		if pending != nil {
			errValue := map[string]json.RawMessage{"error": incoming.Error}
			if len(incoming.NewTabs) > 0 {
				errValue["newTabs"] = incoming.NewTabs
			}
			data, _ := json.Marshal(errValue)
			pending.Ch <- protocol.ExecOutcome{Err: fmt.Errorf("%s", data)}
		}
	}
}
