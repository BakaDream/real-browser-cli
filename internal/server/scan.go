package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/bakadream/real-browser-cli/internal/htmlopt"
)

func ScanPage(state *AppState, tabsOnly bool, switchTabID string, textOnly bool) (map[string]any, error) {
	if err := SetDefaultSession(state, switchTabID); err != nil {
		return nil, err
	}
	tabs := ActiveTabs(state, true)
	if len(tabs) == 0 {
		return nil, rpcCodeError{Code: "tab_not_found", Message: "没有可用的浏览器标签页，查L3记忆分析原因。"}
	}
	state.Driver.Mu.RLock()
	defaultSessionID := state.Driver.DefaultSessionID
	state.Driver.Mu.RUnlock()
	result := map[string]any{
		"tabsCount": len(tabs),
		"tabs":      tabs,
		"activeTab": defaultSessionID,
	}
	if !tabsOnly {
		content, err := GetHTML(state, true, 35000, textOnly)
		if err != nil {
			return nil, err
		}
		result["content"] = content
	}
	return result, nil
}

func GetHTML(state *AppState, cutlist bool, maxchars int, textOnly bool) (string, error) {
	pageScript := fmt.Sprintf("%s\nreturn optHTML(%t);", htmlopt.JSOptHTML(), textOnly)
	response, err := ExecuteRawJS(state, pageScript, 30*time.Second)
	if err != nil {
		return "", err
	}
	var page string
	if len(response.Data) > 0 {
		_ = json.Unmarshal(response.Data, &page)
	}
	if textOnly {
		return htmlopt.CleanText(page), nil
	}
	page = htmlopt.OptimizeHTMLForTokens(page)
	if cutlist {
		listScript := htmlopt.JSFindMainList() + "\nreturn findMainList(document.body);"
		_, _ = ExecuteRawJS(state, listScript, 10*time.Second)
	}
	if len(page) > maxchars {
		page = htmlopt.SmartTruncate(page, maxchars)
	}
	return page, nil
}

func isExtensionJSON(script string) bool {
	trimmed := strings.TrimSpace(script)
	if !strings.HasPrefix(trimmed, "{") {
		return false
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		return false
	}
	_, ok := value["cmd"]
	return ok
}

func wrapScriptWithWait(script, waitJS string, timeout, interval float64) string {
	scriptJSON, _ := json.Marshal(script)
	waitJSON, _ := json.Marshal(waitJS)
	timeoutMS := int64(maxFloat(timeout, 0) * 1000)
	intervalMS := int64(maxFloat(interval, 0.02) * 1000)
	return fmt.Sprintf(`
const __agentBrowserMain = %s;
const __agentBrowserWait = %s;
const __agentBrowserTimeoutMs = %d;
const __agentBrowserIntervalMs = %d;
const AsyncFunction = Object.getPrototypeOf(async function(){}).constructor;
const __runUser = async (code) => {
  const trimmed = String(code || '').trim();
  if (!trimmed) return undefined;
  if (/^return\b/.test(trimmed)) return await (new AsyncFunction(trimmed))();
  try {
    const value = eval(trimmed);
    return value instanceof Promise ? await value : value;
  } catch (e) {
    if (e instanceof SyntaxError && (/return/i.test(e.message) || /await/i.test(e.message))) {
      return await (new AsyncFunction(trimmed))();
    }
    throw e;
  }
};
const __mainResult = await __runUser(__agentBrowserMain);
let __matched = false;
let __waitValue = undefined;
let __waitError = null;
const __deadline = Date.now() + __agentBrowserTimeoutMs;
while (true) {
  try {
    __waitValue = await __runUser(__agentBrowserWait);
    __waitError = null;
    if (__waitValue) { __matched = true; break; }
  } catch (e) {
    __waitError = e.message || String(e);
  }
  if (Date.now() >= __deadline) break;
  await new Promise(resolve => setTimeout(resolve, __agentBrowserIntervalMs));
}
return { result: __mainResult, wait: { ok: __matched, matched: __matched, value: __waitValue, error: __waitError } };
`, scriptJSON, waitJSON, timeoutMS, intervalMS)
}
