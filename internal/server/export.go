package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func buildHAR(state *AppState, includeExtension bool) map[string]any {
	state.dataMu.Lock()
	items := append([]NetworkEntry(nil), state.Network...)
	state.dataMu.Unlock()
	entries := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if !includeExtension && strings.HasPrefix(item.URL, "chrome-extension://") {
			continue
		}
		started := item.StartedAt
		if started.IsZero() {
			started = item.Time
		}
		duration := 0.0
		if !item.FinishedAt.IsZero() && !started.IsZero() {
			duration = float64(item.FinishedAt.Sub(started).Milliseconds())
		}
		entries = append(entries, map[string]any{
			"startedDateTime": started.Format(time.RFC3339Nano),
			"time":            duration,
			"request": map[string]any{
				"method":      firstNonEmptyString(item.Method, "GET"),
				"url":         item.URL,
				"httpVersion": "HTTP/1.1",
				"headers":     harHeaders(item.RequestHeaders),
				"queryString": []any{},
				"cookies":     []any{},
				"headersSize": -1,
				"bodySize":    -1,
			},
			"response": map[string]any{
				"status":      item.Status,
				"statusText":  "",
				"httpVersion": "HTTP/1.1",
				"headers":     harHeaders(item.ResponseHeaders),
				"cookies":     []any{},
				"content": map[string]any{
					"size":     len(item.Body),
					"mimeType": item.MimeType,
				},
				"redirectURL": "",
				"headersSize": -1,
				"bodySize":    -1,
			},
			"cache": map[string]any{},
			"timings": map[string]any{
				"send":    0,
				"wait":    duration,
				"receive": 0,
			},
			"_requestId": item.RequestID,
			"_type":      item.Type,
			"_failed":    item.Failed,
			"_errorText": item.ErrorText,
		})
	}
	return map[string]any{
		"log": map[string]any{
			"version": "1.2",
			"creator": map[string]any{
				"name":    "real-browser-cli",
				"version": "alpha",
			},
			"pages":   []any{},
			"entries": entries,
		},
	}
}

func harHeaders(headers map[string]any) []map[string]string {
	out := make([]map[string]string, 0, len(headers))
	for name, value := range headers {
		out = append(out, map[string]string{"name": name, "value": fmt.Sprint(value)})
	}
	return out
}

func exportPlaywright(state *AppState) string {
	state.dataMu.Lock()
	steps := append([]TraceStep(nil), state.Trace...)
	state.dataMu.Unlock()
	var b strings.Builder
	b.WriteString("import { test, expect } from '@playwright/test';\n\n")
	b.WriteString("test('real-browser trace', async ({ page }) => {\n")
	for _, step := range steps {
		writePlaywrightStep(&b, step)
	}
	b.WriteString("});\n")
	return b.String()
}

func writePlaywrightStep(b *strings.Builder, step TraceStep) {
	switch step.Command {
	case "open":
		if step.URLAfter != "" {
			fmt.Fprintf(b, "  await page.goto(%s);\n", jsonString(step.URLAfter))
		}
	case "action.click", "action.dblclick":
		action := "click"
		if step.Command == "action.dblclick" {
			action = "dblclick"
		}
		fmt.Fprintf(b, "  await %s.%s();\n", playwrightLocator(step), action)
	case "action.fill":
		fmt.Fprintf(b, "  await %s.fill('REDACTED');\n", playwrightLocator(step))
	case "action.press":
		fmt.Fprintf(b, "  await page.keyboard.press(%s);\n", jsonString(stringFromTraceParams(step, "value", "Enter")))
	case "wait":
		if step.Target != "" {
			fmt.Fprintf(b, "  await expect(%s).toBeVisible();\n", playwrightLocator(step))
		} else {
			b.WriteString("  // wait step was recorded; add an explicit Playwright wait here if needed\n")
		}
	case "screenshot":
		b.WriteString("  await page.screenshot({ fullPage: true });\n")
	default:
		fmt.Fprintf(b, "  // %s was recorded but needs manual conversion\n", step.Command)
	}
}

func playwrightLocator(step TraceStep) string {
	if step.Role != "" && step.Name != "" {
		return fmt.Sprintf("page.getByRole(%s, { name: %s })", jsonString(strings.ToLower(step.Role)), jsonString(step.Name))
	}
	if step.Name != "" {
		return fmt.Sprintf("page.getByText(%s)", jsonString(step.Name))
	}
	if step.Target != "" && !strings.HasPrefix(step.Target, "@") {
		return fmt.Sprintf("page.locator(%s)", jsonString(step.Target))
	}
	return "page.locator('body')"
}

func exportDrissionPage(state *AppState) string {
	state.dataMu.Lock()
	steps := append([]TraceStep(nil), state.Trace...)
	state.dataMu.Unlock()
	var b strings.Builder
	b.WriteString("from DrissionPage import ChromiumPage\n\n")
	b.WriteString("page = ChromiumPage()\n")
	for _, step := range steps {
		switch step.Command {
		case "open":
			if step.URLAfter != "" {
				fmt.Fprintf(&b, "page.get(%s)\n", jsonString(step.URLAfter))
			}
		case "action.click":
			fmt.Fprintf(&b, "page.ele(%s).click()\n", jsonString(drissionTarget(step)))
		case "action.fill":
			fmt.Fprintf(&b, "page.ele(%s).input('REDACTED')\n", jsonString(drissionTarget(step)))
		default:
			fmt.Fprintf(&b, "# %s was recorded but needs manual conversion\n", step.Command)
		}
	}
	return b.String()
}

func drissionTarget(step TraceStep) string {
	if step.Target != "" && !strings.HasPrefix(step.Target, "@") {
		return step.Target
	}
	if step.Name != "" {
		return "text:" + step.Name
	}
	return "tag:body"
}

func jsonString(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func stringFromTraceParams(step TraceStep, key string, def string) string {
	if step.Params == nil {
		return def
	}
	if value, ok := step.Params[key]; ok {
		return fmt.Sprint(value)
	}
	return def
}
