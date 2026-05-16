package server

import (
	"strings"
	"time"
	"unicode/utf8"
)

func NormalizeURL(url string) string {
	if strings.Contains(url, "://") || strings.HasPrefix(url, "about:") || strings.HasPrefix(url, "chrome:") || strings.HasPrefix(url, "edge:") || strings.HasPrefix(url, "file:") || strings.HasPrefix(url, "data:") {
		return url
	}
	return "https://" + url
}

func timeSinceSeconds(start time.Time) float64 {
	return time.Since(start).Seconds()
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
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
