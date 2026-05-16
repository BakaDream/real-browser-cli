package server

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/bakadream/real-browser-cli/internal/protocol"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func writeJSON(c *gin.Context, status int, data any) {
	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(data); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.Status(status)
	c.Writer.Write(buf.Bytes())
}

func apiMeta(command string, state *AppState, start time.Time) map[string]any {
	meta := map[string]any{
		"command":    command,
		"durationMs": time.Since(start).Milliseconds(),
		"activeTab":  activeHandle(state),
		"warnings":   []string{},
	}
	return meta
}

func apiSuccess(id string, data any, meta map[string]any) protocol.APIResponse {
	if id == "" {
		id = uuid.NewString()
	}
	if data == nil {
		data = map[string]any{"completed": true}
	}
	return protocol.APIResponse{ID: id, Success: true, Data: data, Meta: meta}
}

func apiFailure(id string, code string, message string, retryable bool, details map[string]any, meta map[string]any) protocol.APIResponse {
	if id == "" {
		id = uuid.NewString()
	}
	if details == nil {
		details = map[string]any{}
	}
	return protocol.APIResponse{
		ID:      id,
		Success: false,
		Error: &protocol.APIError{
			Code:      code,
			Message:   message,
			Retryable: retryable,
			Details:   details,
		},
		Meta: meta,
	}
}

func writeAPIResponse(c *gin.Context, status int, resp protocol.APIResponse) {
	writeJSON(c, status, resp)
}

func writeAPISuccess(c *gin.Context, id string, data any, meta map[string]any) {
	writeAPIResponse(c, http.StatusOK, apiSuccess(id, data, meta))
}

func writeAPIFailure(c *gin.Context, id string, err error, meta map[string]any) {
	resp := rpcError(id, err)
	resp.Meta = meta
	writeAPIResponse(c, statusForAPIError(resp.Error), resp)
}

func statusForAPIError(err *protocol.APIError) int {
	if err == nil {
		return http.StatusInternalServerError
	}
	switch err.Code {
	case "bad_request", "unknown_command":
		return http.StatusBadRequest
	case "unauthorized":
		return http.StatusUnauthorized
	case "tab_not_found", "target_not_found", "request_not_found", "dialog_not_found":
		return http.StatusNotFound
	case "timeout":
		return http.StatusRequestTimeout
	case "bridge_not_connected":
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func badRequestError(err error) rpcCodeError {
	return rpcCodeError{Code: "bad_request", Message: err.Error()}
}

func legacyHTTPData(value map[string]any) map[string]any {
	out := make(map[string]any, len(value))
	for key, item := range value {
		switch key {
		case "status":
			continue
		case "msg":
			out["message"] = item
		default:
			out[lowerCamel(key)] = item
		}
	}
	if len(out) == 0 {
		out["completed"] = true
	}
	return out
}

func lowerCamel(key string) string {
	parts := strings.Split(key, "_")
	if len(parts) == 1 {
		return key
	}
	for i := 1; i < len(parts); i++ {
		if parts[i] == "" {
			continue
		}
		parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}
	return strings.Join(parts, "")
}

type scanRequest struct {
	TabsOnly    bool   `json:"tabs_only"`
	SwitchTabID string `json:"switch_tab_id"`
	TextOnly    bool   `json:"text_only"`
}

type execRequest struct {
	Script       string  `json:"script"`
	SwitchTabID  string  `json:"switch_tab_id"`
	NoMonitor    bool    `json:"no_monitor"`
	WaitJS       string  `json:"wait_js"`
	WaitTimeout  float64 `json:"wait_timeout"`
	WaitInterval float64 `json:"wait_interval"`
}

type openRequest struct {
	URL         string `json:"url"`
	Active      *bool  `json:"active"`
	SwitchTabID string `json:"switch_tab_id"`
}

func NewRouter(state *AppState) http.Handler {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "real-browser-cli")
	})
	r.GET("/health", healthHandler(state))
	r.GET("/v1/health", healthHandler(state))
	authed := r.Group("/", authMiddleware(state))
	authed.GET("/tabs", tabsHandler(state))
	authed.POST("/scan", scanHandler(state))
	authed.POST("/exec", execHandler(state))
	authed.POST("/open", openHandler(state))
	authed.POST("/shutdown", shutdownHandler(state))
	authed.POST("/v1/rpc", rpcHandler(state))
	authed.POST("/v1/shutdown", shutdownHandler(state))
	return r
}

func authMiddleware(state *AppState) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !authorizedRequest(c.Request, state.Token) {
			writeAPIResponse(c, http.StatusUnauthorized, apiFailure("", "unauthorized", "unauthorized", false, nil, map[string]any{"warnings": []string{}}))
			c.Abort()
			return
		}
		c.Next()
	}
}

func healthHandler(state *AppState) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		if !authorizedRequest(c.Request, state.Token) {
			writeAPISuccess(c, "", gin.H{"running": true}, gin.H{"scope": "minimal", "durationMs": time.Since(start).Milliseconds(), "warnings": []string{}})
			return
		}
		ready := len(ActiveTabs(state, false)) > 0
		idleFor := state.IdleFor().Seconds()
		ttl := idleShutdownTTL.Seconds()
		writeAPISuccess(c, "", gin.H{
			"running":      true,
			"ready":        ready,
			"bridge":       HasExtensionSender(state),
			"activeTab":    activeHandle(state),
			"tabsCount":    len(ActiveTabs(state, false)),
			"uptime":       timeSinceSeconds(state.StartedAt),
			"idleFor":      idleFor,
			"ttl":          ttl,
			"ttlRemaining": maxFloat(ttl-idleFor, 0),
		}, apiMeta("health", state, start))
	}
}

func authorizedRequest(r *http.Request, token string) bool {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return false
	}
	got := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	return secureTokenEqual(got, token)
}

func secureTokenEqual(got, want string) bool {
	if got == "" || want == "" || len(got) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func tabsHandler(state *AppState) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		state.Touch()
		tabs := ActiveTabs(state, true)
		writeAPISuccess(c, "", gin.H{"tabs": tabs, "activeTab": activeHandle(state), "tabsCount": len(tabs)}, apiMeta("tabs", state, start))
	}
}

func scanHandler(state *AppState) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		state.Touch()
		var req scanRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			writeAPIFailure(c, "", badRequestError(err), apiMeta("scan", state, start))
			return
		}
		result, err := ScanPage(state, req.TabsOnly, req.SwitchTabID, req.TextOnly)
		if err != nil {
			writeAPIFailure(c, "", err, apiMeta("scan", state, start))
			return
		}
		writeAPISuccess(c, "", legacyHTTPData(result), apiMeta("scan", state, start))
	}
}

func execHandler(state *AppState) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		state.Touch()
		var req execRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			writeAPIFailure(c, "", badRequestError(err), apiMeta("exec", state, start))
			return
		}
		if req.WaitTimeout == 0 {
			req.WaitTimeout = 3
		}
		if req.WaitInterval == 0 {
			req.WaitInterval = 0.1
		}
		script := req.Script
		combinedWait := req.WaitJS != "" && !isExtensionJSON(req.Script)
		if combinedWait {
			script = wrapScriptWithWait(req.Script, req.WaitJS, req.WaitTimeout, req.WaitInterval)
		}
		result, err := ExecutePageJS(state, script, req.SwitchTabID, req.NoMonitor)
		if err != nil {
			writeAPIFailure(c, "", err, apiMeta("exec", state, start))
			return
		}
		meta := apiMeta("exec", state, start)
		meta["combinedWait"] = combinedWait
		writeAPISuccess(c, "", legacyHTTPData(result), meta)
	}
}

func openHandler(state *AppState) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		state.Touch()
		var req openRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			writeAPIFailure(c, "", badRequestError(err), apiMeta("open", state, start))
			return
		}
		active := true
		if req.Active != nil {
			active = *req.Active
		}
		payload, _ := json.Marshal(map[string]any{
			"cmd":    "openTab",
			"url":    NormalizeURL(req.URL),
			"active": active,
		})
		result, err := ExecuteOpenCommand(state, string(payload), req.SwitchTabID)
		if err != nil {
			writeAPIFailure(c, "", err, apiMeta("open", state, start))
			return
		}
		writeAPISuccess(c, "", legacyHTTPData(result), apiMeta("open", state, start))
	}
}

func shutdownHandler(state *AppState) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		state.Touch()
		state.RequestShutdown()
		writeAPISuccess(c, "", gin.H{"status": "shutdownRequested"}, apiMeta("shutdown", state, start))
	}
}
