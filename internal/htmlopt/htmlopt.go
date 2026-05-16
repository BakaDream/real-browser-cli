package htmlopt

import (
	"bytes"
	"embed"
	"encoding/json"
	"strings"
	"unicode/utf8"

	xhtml "golang.org/x/net/html"
)

//go:embed assets/simphtml_opt.js assets/simphtml_find_list.js
var embedded embed.FS

func JSOptHTML() string {
	data, _ := embedded.ReadFile("assets/simphtml_opt.js")
	return string(data)
}

func JSFindMainList() string {
	data, _ := embedded.ReadFile("assets/simphtml_find_list.js")
	return string(data)
}

func CleanText(input string) string {
	lines := strings.Split(input, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

func SmartTruncate(input string, budget int) string {
	if len(input) <= budget {
		return input
	}
	keep := budget - 32
	if keep < 0 {
		keep = 0
	}
	return takeRunes(input, keep) + " [TRUNCATED]"
}

func ChangedElements(beforeHTML, afterHTML string) json.RawMessage {
	if beforeHTML == afterHTML {
		return mustJSON(map[string]any{"changed": 0})
	}
	return mustJSON(map[string]any{
		"changed":    1,
		"top_change": takeRunes(afterHTML, 2000),
	})
}

func OptimizeHTMLForTokens(input string) string {
	root, err := xhtml.Parse(strings.NewReader(input))
	if err != nil {
		return input
	}
	cleanNode(root)
	var buf bytes.Buffer
	if err := xhtml.Render(&buf, root); err != nil {
		return input
	}
	return buf.String()
}

func cleanNode(n *xhtml.Node) {
	if n.Type == xhtml.ElementNode {
		tag := strings.ToLower(n.Data)
		if tag == "svg" {
			n.Attr = nil
			removeChildren(n)
			return
		}
		attrs := n.Attr[:0]
		for _, attr := range n.Attr {
			key := strings.ToLower(attr.Key)
			if !allowedAttr(key) {
				continue
			}
			attr.Key = key
			attr.Val = compactAttr(key, attr.Val)
			attrs = append(attrs, attr)
		}
		n.Attr = attrs
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		cleanNode(child)
	}
}

func removeChildren(n *xhtml.Node) {
	for child := n.FirstChild; child != nil; {
		next := child.NextSibling
		n.RemoveChild(child)
		child = next
	}
}

func allowedAttr(key string) bool {
	switch key {
	case "id", "class", "name", "src", "href", "alt", "value", "type", "placeholder", "disabled", "checked", "selected", "readonly", "required", "multiple", "role", "aria-label", "aria-expanded", "aria-hidden", "contenteditable", "title", "for", "action", "method", "target", "colspan", "rowspan":
		return true
	default:
		return strings.HasPrefix(key, "data-")
	}
}

func compactAttr(key, value string) string {
	if key == "src" {
		if strings.HasPrefix(value, "data:") {
			return "__img__"
		}
		if len(value) > 30 {
			return "__url__"
		}
	}
	if (key == "href" || key == "action") && len(value) > 30 {
		if key == "href" {
			return "__link__"
		}
		return "__url__"
	}
	if (key == "value" || key == "title" || key == "alt") && len(value) > 100 {
		return takeRunes(value, 50) + " ..."
	}
	if strings.HasPrefix(key, "data-") && !strings.HasPrefix(key, "data-v") && len(value) > 20 {
		return "__data__"
	}
	return value
}

func takeRunes(input string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if utf8.RuneCountInString(input) <= limit {
		return input
	}
	var b strings.Builder
	count := 0
	for _, r := range input {
		if count >= limit {
			break
		}
		b.WriteRune(r)
		count++
	}
	return b.String()
}

func mustJSON(value any) json.RawMessage {
	data, _ := json.Marshal(value)
	return data
}
