package htmlopt

import (
	"strings"
	"testing"
)

func TestOptimizeHTMLForTokens(t *testing.T) {
	input := `<div onclick="x()" data-id="abcdefghijklmnopqrstuvwxyz" href="bad"><svg width="1"><path d="x"></path></svg><img src="data:image/png;base64,abc" alt="` + strings.Repeat("a", 120) + `"></div>`
	got := OptimizeHTMLForTokens(input)
	if strings.Contains(got, "onclick") {
		t.Fatalf("onclick should be removed: %s", got)
	}
	if strings.Contains(got, "<path") || strings.Contains(got, `width="1"`) {
		t.Fatalf("svg content/attrs should be removed: %s", got)
	}
	if !strings.Contains(got, `src="__img__"`) {
		t.Fatalf("data image should be compacted: %s", got)
	}
	if !strings.Contains(got, `data-id="__data__"`) {
		t.Fatalf("long data attr should be compacted: %s", got)
	}
}

func TestSmartTruncate(t *testing.T) {
	got := SmartTruncate(strings.Repeat("a", 80), 40)
	if !strings.HasSuffix(got, " [TRUNCATED]") {
		t.Fatalf("expected truncation suffix: %q", got)
	}
}
