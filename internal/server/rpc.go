package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bakadream/real-browser-cli/internal/protocol"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type rpcParams map[string]any

func rpcHandler(state *AppState) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		state.Touch()
		var req protocol.APIRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			writeAPIFailure(c, "", badRequestError(err), map[string]any{"command": "rpc", "durationMs": time.Since(start).Milliseconds(), "warnings": []string{}})
			return
		}
		if req.ID == "" {
			req.ID = uuid.NewString()
		}
		data, duration, err := dispatchRPCWithTrace(state, req)
		meta := map[string]any{
			"command":    req.Command,
			"durationMs": duration.Milliseconds(),
			"activeTab":  activeHandle(state),
			"warnings":   []string{},
		}
		if err != nil {
			if batchErr, ok := err.(batchRPCError); ok {
				resp := apiFailure(req.ID, "batch_failed", batchErr.Error(), false, nil, meta)
				resp.Data = map[string]any{"results": batchErr.Results}
				writeAPIResponse(c, http.StatusOK, resp)
				return
			}
			resp := rpcError(req.ID, err)
			resp.Meta = meta
			writeAPIResponse(c, statusForAPIError(resp.Error), resp)
			return
		}
		writeAPISuccess(c, req.ID, normalizeRPCData(req.Command, data), meta)
	}
}

func dispatchRPCWithTrace(state *AppState, req protocol.APIRequest) (any, time.Duration, error) {
	start := time.Now()
	params := decodeParams(req.Params)
	trace := shouldTraceCommand(req.Command)
	beforeTab, beforeURL := "", ""
	if trace {
		beforeTab, beforeURL = currentTraceContext(state, targetFrom(req, params))
	}
	data, err := dispatchRPC(state, req)
	duration := time.Since(start)
	if trace {
		_, afterURL := currentTraceContext(state, targetFrom(req, params))
		appendTrace(state, buildTraceStep(state, req, params, beforeTab, beforeURL, afterURL, data, err, duration))
	}
	return data, duration, err
}

func dispatchRPC(state *AppState, req protocol.APIRequest) (any, error) {
	params := decodeParams(req.Params)
	switch req.Command {
	case "doctor":
		return doctorData(state), nil
	case "tab.list":
		return map[string]any{"tabs": ActiveTabs(state, true), "activeTab": activeHandle(state)}, nil
	case "tab.new":
		return rpcOpenTab(state, params)
	case "tab.use":
		return rpcTabUse(state, params)
	case "tab.close":
		return rpcTabClose(state, params, targetFrom(req, params))
	case "tab.label":
		return rpcTabLabel(state, params)
	case "open":
		return rpcOpen(state, params, targetFrom(req, params))
	case "back":
		return rpcEval(state, "history.back(); return location.href", targetFrom(req, params), 10*time.Second)
	case "forward":
		return rpcEval(state, "history.forward(); return location.href", targetFrom(req, params), 10*time.Second)
	case "reload":
		return rpcReload(state, targetFrom(req, params), boolParam(params, "hard"))
	case "snapshot":
		return rpcSnapshot(state, targetFrom(req, params), boolParam(params, "locators"), boolParam(params, "text"), stringParam(params, "selector"))
	case "get":
		return rpcGet(state, params, targetFrom(req, params))
	case "action.click", "action.dblclick", "action.hover", "action.focus", "action.fill", "action.type", "action.press", "action.select", "action.check", "action.uncheck", "action.scroll", "action.drag", "action.upload":
		return rpcAction(state, req.Command, params, targetFrom(req, params))
	case "wait":
		return rpcWait(state, params, targetFrom(req, params))
	case "eval":
		return rpcEval(state, stringParam(params, "script"), targetFrom(req, params), timeoutFrom(req, 30*time.Second))
	case "cdp":
		return rpcCDP(state, stringParam(params, "method"), mapParam(params, "params"), targetFrom(req, params))
	case "cookies.list":
		return rpcExtCommand(state, map[string]any{"cmd": "cookies", "url": stringParam(params, "url")}, targetFrom(req, params))
	case "cookies.set":
		return rpcExtCommand(state, map[string]any{"cmd": "setCookie", "details": params}, targetFrom(req, params))
	case "cookies.delete":
		return rpcExtCommand(state, map[string]any{"cmd": "deleteCookie", "url": stringParam(params, "url"), "name": stringParam(params, "name")}, targetFrom(req, params))
	case "cookies.clear":
		return rpcCookiesClear(state, params, targetFrom(req, params))
	case "storage.local.get", "storage.local.set", "storage.local.delete", "storage.local.clear":
		return rpcStorage(state, strings.TrimPrefix(req.Command, "storage.local."), "localStorage", params, targetFrom(req, params))
	case "storage.session.get", "storage.session.set", "storage.session.delete", "storage.session.clear":
		return rpcStorage(state, strings.TrimPrefix(req.Command, "storage.session."), "sessionStorage", params, targetFrom(req, params))
	case "screenshot":
		return rpcScreenshot(state, params, targetFrom(req, params))
	case "pdf":
		return rpcPDF(state, params, targetFrom(req, params))
	case "console.list":
		_, _ = rpcObserveStart(state, targetFrom(req, params), false)
		return rpcConsoleList(state, stringParam(params, "level")), nil
	case "console.clear":
		state.dataMu.Lock()
		state.Console = nil
		state.dataMu.Unlock()
		return map[string]any{"cleared": true}, nil
	case "errors.list":
		_, _ = rpcObserveStart(state, targetFrom(req, params), false)
		state.dataMu.Lock()
		defer state.dataMu.Unlock()
		return map[string]any{"errors": state.Errors}, nil
	case "errors.clear":
		state.dataMu.Lock()
		state.Errors = nil
		state.dataMu.Unlock()
		return map[string]any{"cleared": true}, nil
	case "network.list":
		_, _ = rpcObserveStart(state, targetFrom(req, params), false)
		return rpcNetworkList(state, params), nil
	case "network.get":
		return rpcNetworkGet(state, stringParam(params, "requestId"))
	case "network.clear":
		state.dataMu.Lock()
		state.Network = nil
		state.dataMu.Unlock()
		return map[string]any{"cleared": true}, nil
	case "network.har.start":
		return rpcObserveStart(state, targetFrom(req, params), true)
	case "network.har.stop":
		return rpcObserveStop(state, targetFrom(req, params))
	case "network.har.save":
		return rpcHARSave(state, params)
	case "network.block":
		return rpcNetworkBlock(state, stringParam(params, "pattern"))
	case "network.unblock":
		return rpcNetworkUnblock(state, stringParam(params, "pattern"))
	case "dialog.status":
		state.dataMu.Lock()
		dialog := state.Dialog
		state.dataMu.Unlock()
		if dialog != nil && dialog.Open {
			return dialog, nil
		}
		_, _ = rpcObserveStart(state, targetFrom(req, params), false)
		state.dataMu.Lock()
		defer state.dataMu.Unlock()
		if state.Dialog != nil {
			return state.Dialog, nil
		}
		return map[string]any{"open": false}, nil
	case "dialog.accept":
		return rpcDialogHandle(state, targetFrom(req, params), true, stringParam(params, "prompt"))
	case "dialog.dismiss":
		return rpcDialogHandle(state, targetFrom(req, params), false, "")
	case "trace.show":
		state.dataMu.Lock()
		defer state.dataMu.Unlock()
		return map[string]any{"steps": state.Trace}, nil
	case "trace.clear":
		state.dataMu.Lock()
		state.Trace = nil
		state.dataMu.Unlock()
		return map[string]any{"cleared": true}, nil
	case "export.playwright":
		return map[string]any{"content": exportPlaywright(state)}, nil
	case "export.drissionpage":
		return map[string]any{"content": exportDrissionPage(state)}, nil
	case "batch":
		return rpcBatch(state, params)
	default:
		return nil, rpcCodeError{Code: "unknown_command", Message: "unknown command: " + req.Command}
	}
}

func rpcOpenTab(state *AppState, params rpcParams) (any, error) {
	active := !boolParam(params, "background")
	res, err := rpcExtCommand(state, map[string]any{"cmd": "openTab", "url": NormalizeURL(stringParam(params, "url")), "active": active}, "")
	if err != nil {
		return nil, err
	}
	if label := stringParam(params, "label"); label != "" {
		handle := handleFromOpenResult(state, res)
		if handle == "" {
			return nil, rpcCodeError{Code: "tab_not_found", Message: "new tab was not registered"}
		}
		if _, err := SetTabLabel(state, handle, label); err != nil {
			return nil, err
		}
	}
	return res, nil
}

func handleFromOpenResult(state *AppState, result any) string {
	m, ok := result.(map[string]any)
	if !ok {
		return ""
	}
	id := firstNonEmptyString(stringAny(m["chromeTabId"]), stringAny(m["id"]))
	if id == "" {
		if data, ok := m["data"].(map[string]any); ok {
			id = firstNonEmptyString(stringAny(data["chromeTabId"]), stringAny(data["id"]))
		}
	}
	if id == "" {
		return ""
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		state.Driver.Mu.RLock()
		handle := state.Driver.TabHandles[id]
		state.Driver.Mu.RUnlock()
		if handle != "" || time.Now().After(deadline) {
			return handle
		}
		WaitForSessions(state, 200*time.Millisecond)
		time.Sleep(50 * time.Millisecond)
	}
}

func rpcOpen(state *AppState, params rpcParams, tab string) (any, error) {
	if boolParam(params, "newTab") {
		params["background"] = boolParam(params, "background")
		return rpcOpenTab(state, params)
	}
	sessionID, _, _, err := ResolveSession(state, tab)
	if err != nil {
		return nil, err
	}
	return rpcExtCommand(state, map[string]any{"cmd": "tabs", "method": "navigate", "tabId": parseTabID(sessionID), "url": NormalizeURL(stringParam(params, "url"))}, sessionID)
}

func rpcTabUse(state *AppState, params rpcParams) (any, error) {
	target := firstNonEmptyString(stringParam(params, "tab"), stringParam(params, "target"))
	_, handle, _, err := ResolveSession(state, target)
	if err != nil {
		return nil, err
	}
	return map[string]any{"tabId": handle}, nil
}

func rpcTabClose(state *AppState, params rpcParams, tab string) (any, error) {
	sessionID, handle, _, err := ResolveSession(state, firstNonEmptyString(tab, stringParam(params, "tab")))
	if err != nil {
		return nil, err
	}
	res, err := rpcExtCommand(state, map[string]any{"cmd": "tabs", "method": "close", "tabId": parseTabID(sessionID)}, sessionID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"tabId": handle, "closed": true, "detail": res}, nil
}

func rpcTabLabel(state *AppState, params rpcParams) (any, error) {
	info, err := SetTabLabel(state, stringParam(params, "tab"), stringParam(params, "label"))
	if err != nil {
		return nil, err
	}
	return info, nil
}

func rpcReload(state *AppState, tab string, hard bool) (any, error) {
	if hard {
		return rpcCDP(state, "Page.reload", map[string]any{"ignoreCache": true}, tab)
	}
	return rpcExtCommand(state, map[string]any{"cmd": "tabs", "method": "reload"}, tab)
}

func rpcSnapshot(state *AppState, tab string, includeLocators bool, textOnly bool, selector string) (any, error) {
	_, handle, session, err := ResolveSession(state, tab)
	if err != nil {
		return nil, err
	}
	if err := RequireScriptable(session, firstNonEmptyString(tab, handle)); err != nil {
		return nil, err
	}
	if textOnly {
		text, err := GetHTML(state, false, 100000, true)
		return map[string]any{"tabId": handle, "snapshot": text}, err
	}
	script := snapshotScript(selector)
	raw, err := ExecuteRawJS(state, script, 20*time.Second)
	if err != nil {
		return nil, err
	}
	var rows []ElementRef
	if err := json.Unmarshal(firstRaw(raw.Data, raw.Result), &rows); err != nil {
		return nil, err
	}
	snapshotID := "s_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	state.dataMu.Lock()
	for i := range rows {
		rows[i].Ref = fmt.Sprintf("e%d", i+1)
		rows[i].TabID = handle
		rows[i].SnapshotID = snapshotID
		state.Refs[rows[i].Ref] = rows[i]
	}
	state.dataMu.Unlock()
	lines := []string{fmt.Sprintf("tab %s", handle), ""}
	for _, item := range rows {
		line := fmt.Sprintf("@%s [%s] %q", item.Ref, item.Role, item.Name)
		if item.Required {
			line += " required"
		}
		if item.Disabled {
			line += " disabled"
		}
		if includeLocators && item.Locators != nil {
			line += fmt.Sprintf(" locators=%v", item.Locators)
		}
		lines = append(lines, line)
	}
	return map[string]any{"tabId": handle, "snapshotId": snapshotID, "snapshot": strings.Join(lines, "\n"), "refs": rows}, nil
}

func rpcGet(state *AppState, params rpcParams, tab string) (any, error) {
	target := stringParam(params, "target")
	switch stringParam(params, "kind") {
	case "title":
		_, _, session, err := ResolveSession(state, tab)
		if err != nil {
			return nil, err
		}
		return map[string]any{"title": session.Info.Title}, nil
	case "url":
		_, _, session, err := ResolveSession(state, tab)
		if err != nil {
			return nil, err
		}
		return map[string]any{"url": session.Info.URL}, nil
	case "text":
		if err := requireScriptableTarget(state, tab); err != nil {
			return nil, err
		}
		text, err := GetHTML(state, false, 100000, true)
		return map[string]any{"text": text}, err
	case "html":
		if err := requireScriptableTarget(state, tab); err != nil {
			return nil, err
		}
		html, err := GetHTML(state, false, 9999999, false)
		return map[string]any{"html": html}, err
	case "markdown":
		if err := requireScriptableTarget(state, tab); err != nil {
			return nil, err
		}
		text, err := GetHTML(state, false, 100000, true)
		return map[string]any{"markdown": text}, err
	case "value", "attr", "count", "styles":
		value, err := rpcEval(state, getScript(stringParam(params, "kind"), target, stringParam(params, "name")), tab, 10*time.Second)
		return map[string]any{stringParam(params, "kind"): value}, err
	case "box":
		ref, ok := resolveElementRef(state, target)
		if ok && ref.Box != nil {
			return map[string]any{"box": ref.Box}, nil
		}
		value, err := rpcEval(state, getScript("box", target, ""), tab, 10*time.Second)
		return map[string]any{"box": value}, err
	default:
		return nil, rpcCodeError{Code: "bad_request", Message: "unknown get kind"}
	}
}

func rpcAction(state *AppState, command string, params rpcParams, tab string) (any, error) {
	action := strings.TrimPrefix(command, "action.")
	target := stringParam(params, "target")
	if action == "upload" {
		return rpcUpload(state, target, stringParam(params, "path"), tab)
	}
	if action == "click" || action == "dblclick" {
		_, _ = rpcObserveStart(state, tab, false)
	}
	if action == "type" || action == "press" || action == "scroll" {
		target = ""
	}
	if (action == "click" || action == "dblclick" || action == "hover" || action == "drag") && target != "" {
		if box, ok, err := actionTargetBox(state, target, tab); err != nil {
			return nil, err
		} else if ok {
			x := box.X + box.Width/2
			y := box.Y + box.Height/2
			if action == "hover" {
				_, err := rpcCDP(state, "Input.dispatchMouseEvent", map[string]any{"type": "mouseMoved", "x": x, "y": y}, tab)
				return map[string]any{"action": action, "target": target}, err
			}
			clickCount := 1
			if action == "dblclick" {
				clickCount = 2
			}
			if _, err := rpcCDP(state, "Input.dispatchMouseEvent", map[string]any{"type": "mouseMoved", "x": x, "y": y}, tab); err != nil {
				return nil, err
			}
			if _, err := rpcCDP(state, "Input.dispatchMouseEvent", map[string]any{"type": "mousePressed", "x": x, "y": y, "button": "left", "clickCount": clickCount}, tab); err != nil {
				return nil, err
			}
			_, err := rpcCDP(state, "Input.dispatchMouseEvent", map[string]any{"type": "mouseReleased", "x": x, "y": y, "button": "left", "clickCount": clickCount}, tab)
			return map[string]any{"action": action, "target": target}, err
		}
	}
	script := actionScript(action, target, stringParam(params, "value"), boolParam(params, "clear"), stringParam(params, "path"), floatParam(params, "x"), floatParam(params, "y"))
	value, err := rpcEval(state, script, tab, 20*time.Second)
	out := map[string]any{"action": action}
	if target != "" {
		out["target"] = target
	}
	if action == "select" || action == "check" || action == "uncheck" || action == "scroll" {
		out["value"] = value
	}
	return out, err
}

func actionTargetBox(state *AppState, target string, tab string) (Rect, bool, error) {
	if ref, ok := resolveElementRef(state, target); ok && ref.Box != nil {
		return *ref.Box, true, nil
	}
	value, err := rpcEval(state, getScript("box", target, ""), tab, 5*time.Second)
	if err != nil {
		return Rect{}, false, err
	}
	m, ok := value.(map[string]any)
	if !ok {
		return Rect{}, false, nil
	}
	box := Rect{X: floatFromAny(m["x"]), Y: floatFromAny(m["y"]), Width: floatFromAny(m["width"]), Height: floatFromAny(m["height"])}
	if box.Width <= 0 || box.Height <= 0 {
		return Rect{}, false, rpcCodeError{Code: "target_not_found", Message: "target not found: " + target}
	}
	return box, true, nil
}

func rpcWait(state *AppState, params rpcParams, tab string) (any, error) {
	if ms := intParam(params, "ms"); ms > 0 {
		time.Sleep(time.Duration(ms) * time.Millisecond)
		return map[string]any{"waitedMs": ms}, nil
	}
	timeout := time.Duration(floatParamDefault(params, "timeout", 10) * float64(time.Second))
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var script string
		switch {
		case stringParam(params, "text") != "":
			textJSON, _ := json.Marshal(stringParam(params, "text"))
			script = "return document.body && document.body.innerText.includes(" + string(textJSON) + ")"
		case stringParam(params, "selector") != "":
			selJSON, _ := json.Marshal(stringParam(params, "selector"))
			script = "return !!document.querySelector(" + string(selJSON) + ")"
		case stringParam(params, "ref") != "":
			if _, ok := resolveElementRef(state, stringParam(params, "ref")); ok {
				return map[string]any{"matched": true}, nil
			}
			time.Sleep(200 * time.Millisecond)
			continue
		case stringParam(params, "js") != "":
			script = "return !!(" + stringParam(params, "js") + ")"
		case stringParam(params, "load") != "":
			loadState := stringParam(params, "load")
			if loadState == "networkidle" {
				if networkPending(state, targetFrom(protocol.APIRequest{}, params)) == 0 {
					time.Sleep(500 * time.Millisecond)
					if networkPending(state, targetFrom(protocol.APIRequest{}, params)) == 0 {
						return map[string]any{"matched": true, "load": "networkidle"}, nil
					}
				}
				time.Sleep(200 * time.Millisecond)
				continue
			}
			if loadState == "load" {
				script = waitLoadScript(loadState)
			} else {
				script = waitLoadScript(loadState)
			}
		default:
			return nil, rpcCodeError{Code: "bad_request", Message: "wait condition is required"}
		}
		res, err := rpcEval(state, script, tab, 3*time.Second)
		if err == nil && truthy(res) {
			return map[string]any{"matched": true}, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil, rpcCodeError{Code: "timeout", Message: "wait timeout"}
}

func waitLoadScript(loadState string) string {
	if loadState == "load" {
		return "return document.readyState === 'complete'"
	}
	return "return document.readyState === 'complete' || document.readyState === 'interactive'"
}

func rpcEval(state *AppState, script string, tab string, timeout time.Duration) (any, error) {
	if script == "" {
		return nil, rpcCodeError{Code: "bad_request", Message: "script is required"}
	}
	_, handle, session, err := ResolveSession(state, tab)
	if err != nil {
		return nil, err
	}
	if err := RequireScriptable(session, firstNonEmptyString(tab, handle)); err != nil {
		return nil, err
	}
	res, err := ExecuteRawJSCommand(state, script, timeout, "eval")
	if err != nil {
		if strings.Contains(err.Error(), "target not found") {
			return nil, rpcCodeError{Code: "target_not_found", Message: err.Error()}
		}
		return nil, rpcCodeError{Code: "js_failed", Message: err.Error()}
	}
	return rawValue(firstRaw(res.Data, res.Result)), nil
}

func requireScriptableTarget(state *AppState, tab string) error {
	_, handle, session, err := ResolveSession(state, tab)
	if err != nil {
		return err
	}
	return RequireScriptable(session, firstNonEmptyString(tab, handle))
}

func rpcCDP(state *AppState, method string, params map[string]any, tab string) (any, error) {
	if method == "" {
		return nil, rpcCodeError{Code: "bad_request", Message: "method is required"}
	}
	return rpcExtCommand(state, map[string]any{"cmd": "cdp", "method": method, "params": params}, tab)
}

func rpcExtCommand(state *AppState, payload map[string]any, tab string) (any, error) {
	if tab != "" {
		sessionID, _, _, err := ResolveSession(state, tab)
		if err != nil {
			return nil, err
		}
		if _, exists := payload["tabId"]; !exists {
			payload["tabId"] = parseTabID(sessionID)
		}
	}
	data, _ := json.Marshal(payload)
	command := stringAny(payload["cmd"])
	if command == "cdp" {
		command = "cdp." + stringAny(payload["method"])
	}
	if command == "" {
		command = "ext"
	}
	res, err := executeRawWithAnySenderCommand(state, string(data), 30*time.Second, command)
	if err != nil {
		return nil, err
	}
	return unwrapExtensionResult(rawValue(firstRaw(res.Data, res.Result)))
}

func unwrapExtensionResult(value any) (any, error) {
	m, ok := value.(map[string]any)
	if !ok {
		return value, nil
	}
	if okValue, exists := m["ok"]; exists {
		if success, _ := okValue.(bool); !success {
			message := stringAny(m["error"])
			if message == "" {
				message = "browser extension command failed"
			}
			code := "browser_extension_error"
			if strings.Contains(message, "navigation_failed") {
				code = "navigation_failed"
			}
			return nil, rpcCodeError{Code: code, Message: message}
		}
		if data, exists := m["data"]; exists {
			return data, nil
		}
		if results, exists := m["results"]; exists {
			return map[string]any{"results": results}, nil
		}
		return map[string]any{"completed": true}, nil
	}
	return value, nil
}

func rpcCookiesClear(state *AppState, params rpcParams, tab string) (any, error) {
	list, err := rpcExtCommand(state, map[string]any{"cmd": "cookies", "url": stringParam(params, "url")}, tab)
	if err != nil {
		return nil, err
	}
	items, _ := list.([]any)
	removed := 0
	for _, item := range items {
		cookie, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := cookie["name"].(string)
		url := stringParam(params, "url")
		if url == "" {
			domain, _ := cookie["domain"].(string)
			path, _ := cookie["path"].(string)
			url = "https://" + strings.TrimPrefix(domain, ".") + path
		}
		_, _ = rpcExtCommand(state, map[string]any{"cmd": "deleteCookie", "url": url, "name": name}, tab)
		removed++
	}
	return map[string]any{"removed": removed}, nil
}

func rpcStorage(state *AppState, op string, storage string, params rpcParams, tab string) (any, error) {
	keyJSON, _ := json.Marshal(stringParam(params, "key"))
	valueJSON, _ := json.Marshal(stringParam(params, "value"))
	var script string
	switch op {
	case "get":
		script = fmt.Sprintf("return %s.getItem(%s)", storage, keyJSON)
	case "set":
		script = fmt.Sprintf("%s.setItem(%s, %s); return true", storage, keyJSON, valueJSON)
	case "delete":
		script = fmt.Sprintf("%s.removeItem(%s); return true", storage, keyJSON)
	case "clear":
		script = fmt.Sprintf("%s.clear(); return true", storage)
	}
	return rpcEval(state, script, tab, 10*time.Second)
}

func rpcUpload(state *AppState, target string, path string, tab string) (any, error) {
	if target == "" || path == "" {
		return nil, rpcCodeError{Code: "bad_request", Message: "upload requires target and path"}
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(absPath); err != nil {
		return nil, err
	}
	if strings.HasPrefix(target, "@") {
		ref, ok := resolveElementRef(state, target)
		if !ok || ref.Locators == nil {
			return nil, rpcCodeError{Code: "target_not_found", Message: "upload ref has no locator: " + target}
		}
		if css, _ := ref.Locators["css"].(string); css != "" {
			target = css
		} else {
			return nil, rpcCodeError{Code: "target_not_found", Message: "upload ref has no CSS locator: " + target}
		}
	}
	sessionID, _, _, err := ResolveSession(state, tab)
	if err != nil {
		return nil, err
	}
	res, err := rpcExtCommand(state, map[string]any{"cmd": "upload", "selector": target, "files": []string{absPath}}, sessionID)
	if err != nil {
		return nil, err
	}
	verified, verifyErr := rpcEval(state, fmt.Sprintf("const el=document.querySelector(%s); return !!el && el.files && el.files.length > 0;", jsonString(target)), sessionID, 5*time.Second)
	if verifyErr != nil || !truthy(verified) {
		return nil, rpcCodeError{Code: "upload_failed", Message: "file input did not receive file: " + target}
	}
	return map[string]any{"tabId": state.Driver.TabHandles[sessionID], "target": target, "path": absPath, "detail": res}, nil
}

func rpcScreenshot(state *AppState, params rpcParams, tab string) (any, error) {
	annotate := boolParam(params, "annotate")
	if annotate {
		if _, err := rpcEval(state, annotationScript(), tab, 5*time.Second); err != nil {
			return nil, err
		}
		defer func() {
			_, _ = rpcEval(state, "document.getElementById('__real_browser_ref_overlay')?.remove(); return true;", tab, 3*time.Second)
		}()
	}
	data, err := rpcCDP(state, "Page.captureScreenshot", map[string]any{"format": "png", "captureBeyondViewport": boolParam(params, "full")}, tab)
	if err != nil {
		return nil, err
	}
	path := stringParam(params, "path")
	if path != "" {
		if m, ok := data.(map[string]any); ok {
			if s, _ := m["data"].(string); s != "" {
				png, err := base64.StdEncoding.DecodeString(s)
				if err == nil {
					if err := os.WriteFile(path, png, 0o644); err != nil {
						return nil, err
					}
					return map[string]any{"path": path}, nil
				}
			}
		}
	}
	return data, nil
}

func rpcPDF(state *AppState, params rpcParams, tab string) (any, error) {
	data, err := rpcCDP(state, "Page.printToPDF", map[string]any{"printBackground": true}, tab)
	if err != nil {
		return nil, err
	}
	path := stringParam(params, "path")
	if path != "" {
		if m, ok := data.(map[string]any); ok {
			if s, _ := m["data"].(string); s != "" {
				pdf, err := base64.StdEncoding.DecodeString(s)
				if err == nil {
					if err := os.WriteFile(path, pdf, 0o644); err != nil {
						return nil, err
					}
					return map[string]any{"path": path}, nil
				}
			}
		}
	}
	return data, nil
}

func rpcConsoleList(state *AppState, level string) map[string]any {
	level = normalizeConsoleLevel(level)
	state.dataMu.Lock()
	defer state.dataMu.Unlock()
	out := make([]ConsoleEntry, 0, len(state.Console))
	for _, item := range state.Console {
		if level == "" || item.Level == level {
			out = append(out, item)
		}
	}
	return map[string]any{"console": out}
}

func normalizeConsoleLevel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "warn":
		return "warning"
	default:
		return strings.ToLower(strings.TrimSpace(level))
	}
}

func rpcNetworkList(state *AppState, params rpcParams) map[string]any {
	state.dataMu.Lock()
	defer state.dataMu.Unlock()
	status := stringParam(params, "status")
	typ := stringParam(params, "type")
	includeExtension := boolParam(params, "includeExtension")
	out := make([]NetworkEntry, 0, len(state.Network))
	for _, item := range state.Network {
		if !includeExtension && strings.HasPrefix(item.URL, "chrome-extension://") {
			continue
		}
		if typ != "" && item.Type != typ {
			continue
		}
		if status != "" && !matchStatus(status, item.Status) {
			continue
		}
		out = append(out, item)
	}
	return map[string]any{"requests": out}
}

func rpcNetworkGet(state *AppState, requestID string) (any, error) {
	state.dataMu.Lock()
	var found *NetworkEntry
	for _, item := range state.Network {
		if item.RequestID == requestID {
			cp := item
			found = &cp
			break
		}
	}
	state.dataMu.Unlock()
	if found != nil {
		if found.Body == "" && found.BodyAvailable && found.ChromeTabID != 0 {
			body, err := rpcExtCommand(state, map[string]any{"cmd": "network.getResponseBody", "requestId": requestID}, fmt.Sprintf("%d", found.ChromeTabID))
			if err == nil {
				if m, ok := body.(map[string]any); ok {
					if b, _ := m["body"].(string); b != "" {
						found.Body = b
					}
					if encoded, _ := m["base64Encoded"].(bool); encoded {
						found.Base64Encoded = encoded
					}
				}
			}
		}
		return found, nil
	}
	return nil, rpcCodeError{Code: "target_not_found", Message: "request not found: " + requestID}
}

func rpcHARSave(state *AppState, params rpcParams) (any, error) {
	path := stringParam(params, "path")
	if path == "" {
		return nil, rpcCodeError{Code: "bad_request", Message: "path is required"}
	}
	data, _ := json.MarshalIndent(buildHAR(state, boolParam(params, "includeExtension")), "", "  ")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return nil, err
	}
	return map[string]any{"path": path}, nil
}

func rpcObserveStart(state *AppState, tab string, clear bool) (any, error) {
	sessionID, handle, _, err := ResolveSession(state, tab)
	if err != nil {
		return nil, err
	}
	if clear {
		state.dataMu.Lock()
		filtered := state.Network[:0]
		for _, item := range state.Network {
			if item.TabID != handle {
				filtered = append(filtered, item)
			}
		}
		state.Network = filtered
		state.dataMu.Unlock()
	}
	res, err := rpcExtCommand(state, map[string]any{"cmd": "observe.start", "tabId": parseTabID(sessionID), "domains": []string{"Runtime", "Network", "Page"}}, sessionID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"recording": true, "tabId": handle, "detail": res}, nil
}

func rpcObserveStop(state *AppState, tab string) (any, error) {
	sessionID, handle, _, err := ResolveSession(state, tab)
	if err != nil {
		return nil, err
	}
	res, err := rpcExtCommand(state, map[string]any{"cmd": "observe.stop", "tabId": parseTabID(sessionID)}, sessionID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"recording": false, "tabId": handle, "detail": res}, nil
}

func rpcNetworkBlock(state *AppState, pattern string) (any, error) {
	if pattern == "" {
		return nil, rpcCodeError{Code: "bad_request", Message: "pattern is required"}
	}
	state.dataMu.Lock()
	ruleID := state.DNRRules[pattern]
	if ruleID == 0 {
		ruleID = state.NextDNRRule
		state.NextDNRRule++
		state.DNRRules[pattern] = ruleID
	}
	state.dataMu.Unlock()
	res, err := rpcExtCommand(state, map[string]any{"cmd": "network.block", "pattern": pattern, "ruleId": ruleID}, "")
	if err != nil {
		return nil, err
	}
	return map[string]any{"pattern": pattern, "ruleId": ruleID, "blocked": true, "detail": res}, nil
}

func rpcNetworkUnblock(state *AppState, pattern string) (any, error) {
	if pattern == "" {
		return nil, rpcCodeError{Code: "bad_request", Message: "pattern is required"}
	}
	state.dataMu.Lock()
	ruleID := state.DNRRules[pattern]
	if ruleID != 0 {
		delete(state.DNRRules, pattern)
	}
	state.dataMu.Unlock()
	if ruleID == 0 {
		return nil, rpcCodeError{Code: "target_not_found", Message: "network block rule not found: " + pattern}
	}
	res, err := rpcExtCommand(state, map[string]any{"cmd": "network.unblock", "pattern": pattern, "ruleId": ruleID}, "")
	if err != nil {
		return nil, err
	}
	return map[string]any{"pattern": pattern, "ruleId": ruleID, "blocked": false, "detail": res}, nil
}

func rpcDialogHandle(state *AppState, tab string, accept bool, prompt string) (any, error) {
	state.dataMu.Lock()
	hasDialog := state.Dialog != nil && state.Dialog.Open
	state.dataMu.Unlock()
	if !hasDialog {
		return nil, rpcCodeError{Code: "dialog_not_found", Message: "no dialog is showing"}
	}
	params := map[string]any{"accept": accept}
	if accept && prompt != "" {
		params["promptText"] = prompt
	}
	res, err := rpcCDP(state, "Page.handleJavaScriptDialog", params, tab)
	if err != nil {
		return nil, err
	}
	state.dataMu.Lock()
	state.Dialog = nil
	state.dataMu.Unlock()
	return map[string]any{"handled": true, "accept": accept, "detail": res}, nil
}

func rpcBatch(state *AppState, params rpcParams) (any, error) {
	items, _ := params["items"].([]any)
	bail := boolParam(params, "bail")
	results := make([]any, 0, len(items))
	failed := false
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		req, err := batchItemRequest(m)
		if err != nil {
			failed = true
			results = append(results, rpcError(uuid.NewString(), err))
			if bail {
				break
			}
			continue
		}
		req.ID = uuid.NewString()
		start := time.Now()
		data, err := dispatchRPC(state, req)
		duration := time.Since(start)
		meta := map[string]any{
			"command":    req.Command,
			"durationMs": duration.Milliseconds(),
			"activeTab":  activeHandle(state),
			"warnings":   []string{},
		}
		if err != nil {
			failed = true
			resp := rpcError(req.ID, err)
			resp.Meta = meta
			results = append(results, resp)
			if bail {
				break
			}
			continue
		}
		results = append(results, apiSuccess(req.ID, normalizeRPCData(req.Command, data), meta))
	}
	if failed {
		return nil, batchRPCError{Results: results}
	}
	return map[string]any{"results": results}, nil
}

type batchRPCError struct {
	Results []any
}

func (e batchRPCError) Error() string { return "one or more batch items failed" }

func batchItemRequest(m map[string]any) (protocol.APIRequest, error) {
	if command := stringAny(m["command"]); command != "" {
		return protocol.APIRequest{Command: command, Params: mustJSONRaw(m["params"]), Target: batchTarget(m)}, nil
	}
	cmd := stringAny(m["cmd"])
	if cmd == "" {
		return protocol.APIRequest{}, rpcCodeError{Code: "bad_request", Message: "batch item requires command or cmd"}
	}
	args, _ := m["args"].([]any)
	params := rpcParams{}
	command := ""
	switch cmd {
	case "open":
		command = "open"
		if len(args) > 0 {
			params["url"] = stringAny(args[0])
		}
	case "get":
		command = "get"
		if len(args) > 0 {
			params["kind"] = stringAny(args[0])
		}
		if len(args) > 1 {
			params["target"] = stringAny(args[1])
		}
		if len(args) > 2 {
			params["name"] = stringAny(args[2])
		}
	case "click", "dblclick", "hover", "focus", "check", "uncheck":
		command = "action." + cmd
		if len(args) > 0 {
			params["target"] = stringAny(args[0])
		}
	case "fill", "select":
		command = "action." + cmd
		if len(args) > 0 {
			params["target"] = stringAny(args[0])
		}
		if len(args) > 1 {
			params["value"] = stringAny(args[1])
		}
	case "type", "press":
		command = "action." + cmd
		if len(args) > 0 {
			params["value"] = stringAny(args[0])
		}
	case "upload":
		command = "action.upload"
		if len(args) > 0 {
			params["target"] = stringAny(args[0])
		}
		if len(args) > 1 {
			params["path"] = stringAny(args[1])
		}
	case "wait":
		command = "wait"
		if len(args) > 0 {
			params["ms"] = intFromAny(args[0])
		}
	default:
		return protocol.APIRequest{}, rpcCodeError{Code: "unknown_command", Message: "unknown batch cmd: " + cmd}
	}
	if itemParams, ok := m["params"].(map[string]any); ok {
		for k, v := range itemParams {
			params[k] = v
		}
	}
	return protocol.APIRequest{Command: command, Params: mustJSONRaw(params), Target: batchTarget(m)}, nil
}

func batchTarget(m map[string]any) map[string]any {
	target := map[string]any{}
	if tab := stringAny(m["tab"]); tab != "" {
		target["tab"] = tab
	}
	if t, ok := m["target"].(map[string]any); ok {
		for k, v := range t {
			target[k] = v
		}
	}
	return target
}

func mustJSONRaw(value any) json.RawMessage {
	data, _ := json.Marshal(value)
	return data
}

type rpcCodeError struct {
	Code    string
	Message string
}

func (e rpcCodeError) Error() string { return e.Message }

func rpcError(id string, err error) protocol.APIResponse {
	code := "internal_error"
	retryable := false
	if e, ok := err.(rpcCodeError); ok {
		code = e.Code
		err = fmt.Errorf("%s", e.Message)
	}
	switch {
	case code != "internal_error":
	case err.Error() == "tab_not_found":
		code = "tab_not_found"
	case strings.Contains(err.Error(), "target not found"):
		code = "target_not_found"
	case strings.Contains(err.Error(), "tab not found"):
		code = "tab_not_found"
	case strings.Contains(err.Error(), "label_conflict"):
		code = "label_conflict"
	case strings.Contains(err.Error(), "bridge_not_connected"), strings.Contains(err.Error(), "扩展未连接"), strings.Contains(err.Error(), "浏览器扩展连接已断开"):
		code = "bridge_not_connected"
		retryable = true
	case strings.Contains(err.Error(), "navigation_failed"):
		code = "navigation_failed"
	case strings.Contains(err.Error(), "No dialog is showing"):
		code = "dialog_not_found"
	case strings.Contains(err.Error(), "timeout"):
		code = "timeout"
		retryable = true
	}
	return apiFailure(id, code, err.Error(), retryable, nil, nil)
}

func normalizeRPCData(command string, data any) any {
	if data == nil {
		return map[string]any{"command": command, "completed": true}
	}
	if command == "cookies.list" {
		if _, ok := data.(map[string]any); !ok {
			return map[string]any{"cookies": data}
		}
	}
	if command == "eval" {
		return map[string]any{"value": data}
	}
	if strings.HasPrefix(command, "storage.") && strings.HasSuffix(command, ".get") {
		return map[string]any{"value": data}
	}
	switch v := data.(type) {
	case map[string]any:
		if len(v) == 0 {
			return map[string]any{"command": command, "completed": true}
		}
		return v
	case string:
		return map[string]any{"value": v}
	case bool, float64, int, json.Number:
		return map[string]any{"value": v}
	default:
		return data
	}
}

func decodeParams(raw json.RawMessage) rpcParams {
	if len(raw) == 0 {
		return rpcParams{}
	}
	var params rpcParams
	_ = json.Unmarshal(raw, &params)
	if params == nil {
		return rpcParams{}
	}
	return params
}

func targetFrom(req protocol.APIRequest, params rpcParams) string {
	if v, ok := req.Target["tab"]; ok {
		return stringAny(v)
	}
	return stringParam(params, "tab")
}

func timeoutFrom(req protocol.APIRequest, def time.Duration) time.Duration {
	if req.Options.TimeoutMS > 0 {
		return time.Duration(req.Options.TimeoutMS) * time.Millisecond
	}
	return def
}

func activeHandle(state *AppState) string {
	state.Driver.Mu.RLock()
	defer state.Driver.Mu.RUnlock()
	if state.Driver.ActiveHandle != "" {
		return state.Driver.ActiveHandle
	}
	return activeHandleLocked(state.Driver)
}

func doctorData(state *AppState) map[string]any {
	return map[string]any{
		"running":      true,
		"ready":        HasActiveSessions(state),
		"bridge":       HasExtensionSender(state),
		"tabsCount":    len(ActiveTabs(state, false)),
		"activeTab":    activeHandle(state),
		"capabilities": capabilities(state),
	}
}

func capabilities(state *AppState) map[string]bool {
	state.Driver.Mu.RLock()
	defer state.Driver.Mu.RUnlock()
	out := make(map[string]bool, len(state.Driver.Capabilities))
	for k, v := range state.Driver.Capabilities {
		out[k] = v
	}
	return out
}

func resolveElementRef(state *AppState, target string) (ElementRef, bool) {
	target = strings.TrimPrefix(strings.TrimSpace(target), "@")
	state.dataMu.Lock()
	defer state.dataMu.Unlock()
	ref, ok := state.Refs[target]
	return ref, ok
}

func firstRaw(values ...json.RawMessage) json.RawMessage {
	for _, value := range values {
		if len(value) > 0 {
			return value
		}
	}
	return nil
}

func rawValue(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err == nil {
		return value
	}
	return string(raw)
}

func snapshotScript(selector string) string {
	scope := "document.body"
	if selector != "" {
		sel, _ := json.Marshal(selector)
		scope = "document.querySelector(" + string(sel) + ") || document.body"
	}
	return fmt.Sprintf(`
const root = %s;
const visible = (el) => {
  const r = el.getBoundingClientRect();
  const s = getComputedStyle(el);
  return r.width > 0 && r.height > 0 && s.visibility !== 'hidden' && s.display !== 'none';
};
const cssPath = (el) => {
  if (el.id) return '#' + CSS.escape(el.id);
  const parts = [];
  while (el && el.nodeType === 1 && el !== document.body) {
    let p = el.localName;
    if (el.getAttribute('data-testid')) { p += '[data-testid="' + el.getAttribute('data-testid').replaceAll('"','\\"') + '"]'; parts.unshift(p); break; }
    let n = 1, x = el;
    while ((x = x.previousElementSibling)) if (x.localName === el.localName) n++;
    p += ':nth-of-type(' + n + ')';
    parts.unshift(p);
    el = el.parentElement;
  }
  return parts.join(' > ');
};
const roleOf = (el) => {
  if (el.getAttribute('role')) return el.getAttribute('role');
  if (el.tagName === 'INPUT') {
    const type = (el.getAttribute('type') || 'text').toLowerCase();
    if (type === 'checkbox') return 'checkbox';
    if (type === 'radio') return 'radio';
    if (type === 'button' || type === 'submit' || type === 'reset' || type === 'file') return 'button';
    return 'textbox';
  }
  return ({A:'link',BUTTON:'button',TEXTAREA:'textbox',SELECT:'combobox',IMG:'img',H1:'heading',H2:'heading',H3:'heading'}[el.tagName] || el.tagName.toLowerCase());
};
const nameOf = (el) => el.getAttribute('aria-label') || el.getAttribute('placeholder') || el.innerText || el.value || el.alt || el.title || '';
const candidates = Array.from(root.querySelectorAll('a,button,input,textarea,select,[role],[aria-label],[data-testid],h1,h2,h3,h4,h5,h6,img,label,[contenteditable="true"]')).filter(visible).slice(0, 200);
return candidates.map(el => {
  const r = el.getBoundingClientRect();
  return {
    role: roleOf(el),
    name: String(nameOf(el)).trim().slice(0, 160),
    value: el.value || '',
    required: !!el.required,
    disabled: !!el.disabled || el.getAttribute('aria-disabled') === 'true',
    checked: !!el.checked || el.getAttribute('aria-checked') === 'true',
    selected: !!el.selected || el.getAttribute('aria-selected') === 'true',
    box: {x:r.x, y:r.y, width:r.width, height:r.height},
    locators: {css: cssPath(el), testId: el.getAttribute('data-testid') || '', role: roleOf(el), name: String(nameOf(el)).trim().slice(0, 160)}
  };
});
`, scope)
}

func annotationScript() string {
	return `
document.getElementById('__real_browser_ref_overlay')?.remove();
const root = document.createElement('div');
root.id = '__real_browser_ref_overlay';
root.style.cssText = 'position:fixed;inset:0;z-index:2147483647;pointer-events:none;font:12px sans-serif;';
document.documentElement.appendChild(root);
const items = Array.from(document.querySelectorAll('a,button,input,textarea,select,[role],[aria-label],[data-testid],h1,h2,h3,h4,h5,h6,img,label,[contenteditable="true"]')).slice(0, 120);
let i = 1;
for (const el of items) {
  const r = el.getBoundingClientRect();
  const s = getComputedStyle(el);
  if (r.width <= 0 || r.height <= 0 || s.display === 'none' || s.visibility === 'hidden') continue;
  const box = document.createElement('div');
  box.style.cssText = 'position:fixed;box-sizing:border-box;border:2px solid #ff2d55;background:rgba(255,45,85,.08);left:'+r.left+'px;top:'+r.top+'px;width:'+r.width+'px;height:'+r.height+'px;';
  const tag = document.createElement('div');
  tag.textContent = '@e' + i++;
  tag.style.cssText = 'position:absolute;left:0;top:-18px;background:#ff2d55;color:white;padding:1px 4px;border-radius:3px;font-weight:700;';
  box.appendChild(tag);
  root.appendChild(box);
}
return true;
`
}

func getScript(kind, target, name string) string {
	targetJSON, _ := json.Marshal(target)
	nameJSON, _ := json.Marshal(name)
	prefix := fmt.Sprintf("const el = resolveTarget(%s); if (!el) throw new Error('target not found');", targetJSON)
	resolver := `function resolveTarget(t){ if(!t) return document.activeElement; if(t.startsWith('@')) return null; if(t.startsWith('text=')){ const q=t.slice(5).replace(/^"|"$/g,''); return Array.from(document.querySelectorAll('body *')).find(e => (e.innerText||'').includes(q)); } return document.querySelector(t); }`
	switch kind {
	case "value":
		return resolver + prefix + " return el.value ?? el.textContent ?? '';"
	case "attr":
		return resolver + prefix + fmt.Sprintf(" return el.getAttribute(%s);", nameJSON)
	case "count":
		return fmt.Sprintf("return document.querySelectorAll(%s).length;", targetJSON)
	case "styles":
		return resolver + prefix + " const s=getComputedStyle(el); return {display:s.display, visibility:s.visibility, color:s.color, backgroundColor:s.backgroundColor};"
	case "box":
		return resolver + prefix + " const r=el.getBoundingClientRect(); return {x:r.x,y:r.y,width:r.width,height:r.height};"
	default:
		return "return null"
	}
}

func actionScript(action, target, value string, clear bool, path string, x, y float64) string {
	targetJSON, _ := json.Marshal(target)
	valueJSON, _ := json.Marshal(value)
	pathJSON, _ := json.Marshal(path)
	resolver := `function resolveTarget(t){ if(!t) return document.activeElement; if(t.startsWith('text=')){ const q=t.slice(5).replace(/^"|"$/g,''); return Array.from(document.querySelectorAll('body *')).find(e => (e.innerText||'').includes(q)); } if(t.startsWith('role=')){ const m=t.match(/^role=([^\[]+)(?:\[name="(.*)"\])?/); return Array.from(document.querySelectorAll('body *')).find(e => ((e.getAttribute('role')||e.tagName.toLowerCase())===m[1]) && (!m[2] || (e.innerText||e.value||e.getAttribute('aria-label')||'').includes(m[2]))); } return document.querySelector(t); }`
	prefix := fmt.Sprintf("const el = resolveTarget(%s); if (!el) throw new Error('target not found');", targetJSON)
	switch action {
	case "click", "dblclick", "hover", "focus":
		method := map[string]string{"click": "click", "dblclick": "dblclick", "hover": "mouseover", "focus": "focus"}[action]
		if action == "hover" {
			return resolver + prefix + " el.dispatchEvent(new MouseEvent('mouseover', {bubbles:true})); return true;"
		}
		return resolver + prefix + " el." + method + "(); return true;"
	case "fill":
		if clear {
			return resolver + prefix + fmt.Sprintf(" el.focus(); el.value=''; el.value=%s; el.dispatchEvent(new InputEvent('input',{bubbles:true,inputType:'insertText'})); el.dispatchEvent(new Event('change',{bubbles:true})); return true;", valueJSON)
		}
		return resolver + prefix + fmt.Sprintf(" el.focus(); el.value=%s; el.dispatchEvent(new InputEvent('input',{bubbles:true,inputType:'insertText'})); el.dispatchEvent(new Event('change',{bubbles:true})); return true;", valueJSON)
	case "type":
		return fmt.Sprintf("document.activeElement && document.activeElement.focus(); document.execCommand('insertText', false, %s); return true;", valueJSON)
	case "press":
		return fmt.Sprintf("document.activeElement.dispatchEvent(new KeyboardEvent('keydown',{key:%s,bubbles:true})); document.activeElement.dispatchEvent(new KeyboardEvent('keyup',{key:%s,bubbles:true})); return true;", valueJSON, valueJSON)
	case "select":
		return resolver + prefix + fmt.Sprintf(" el.value=%s; el.dispatchEvent(new Event('input',{bubbles:true})); el.dispatchEvent(new Event('change',{bubbles:true})); return el.value;", valueJSON)
	case "check", "uncheck":
		checked := action == "check"
		return resolver + prefix + fmt.Sprintf(" el.checked=%t; el.dispatchEvent(new Event('input',{bubbles:true})); el.dispatchEvent(new Event('change',{bubbles:true})); return el.checked;", checked)
	case "scroll":
		if target != "" {
			return resolver + prefix + " el.scrollIntoView({block:'center', inline:'center'}); return true;"
		}
		return fmt.Sprintf("window.scrollBy(%f,%f); return {x:scrollX,y:scrollY};", x, y)
	case "upload":
		return resolver + prefix + fmt.Sprintf(" return {path:%s};", pathJSON)
	default:
		return "return null"
	}
}

func truthy(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return v == "true"
	case map[string]any:
		if b, ok := v["value"].(bool); ok {
			return b
		}
	}
	return value != nil
}

func stringParam(params rpcParams, key string) string { return stringAny(params[key]) }

func stringAny(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

func boolParam(params rpcParams, key string) bool {
	v, _ := params[key].(bool)
	return v
}

func intParam(params rpcParams, key string) int {
	switch v := params[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		n, _ := strconv.Atoi(v)
		return n
	default:
		return 0
	}
}

func floatParam(params rpcParams, key string) float64 {
	v, _ := params[key].(float64)
	return v
}

func floatFromAny(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case json.Number:
		n, _ := v.Float64()
		return n
	default:
		return 0
	}
}

func floatParamDefault(params rpcParams, key string, def float64) float64 {
	if v := floatParam(params, key); v != 0 {
		return v
	}
	return def
}

func mapParam(params rpcParams, key string) map[string]any {
	if v, ok := params[key].(map[string]any); ok {
		return v
	}
	return map[string]any{}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func matchStatus(filter string, status int) bool {
	if len(filter) == 3 && strings.HasSuffix(filter, "xx") {
		return strconv.Itoa(status)[0] == filter[0]
	}
	n, _ := strconv.Atoi(filter)
	return n == status
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
