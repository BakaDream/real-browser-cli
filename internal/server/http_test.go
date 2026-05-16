package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPAuthMiddleware(t *testing.T) {
	router := NewRouter(NewAppState("test-token"))

	unauthorized := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tabs", nil)
	router.ServeHTTP(unauthorized, req)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized status, got %d", unauthorized.Code)
	}
	var unauthorizedBody map[string]any
	if err := json.Unmarshal(unauthorized.Body.Bytes(), &unauthorizedBody); err != nil {
		t.Fatal(err)
	}
	if unauthorizedBody["success"] != false {
		t.Fatalf("expected unauthorized unified failure: %v", unauthorizedBody)
	}
	if _, ok := unauthorizedBody["ok"]; ok {
		t.Fatalf("response should not contain top-level ok: %v", unauthorizedBody)
	}
	if _, ok := unauthorizedBody["version"]; ok {
		t.Fatalf("response should not contain top-level version: %v", unauthorizedBody)
	}
	errBody, _ := unauthorizedBody["error"].(map[string]any)
	if errBody["code"] != "unauthorized" || errBody["message"] == "" {
		t.Fatalf("expected unified error object: %v", unauthorizedBody)
	}

	minimalHealth := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/health", nil)
	router.ServeHTTP(minimalHealth, req)
	if minimalHealth.Code != http.StatusOK {
		t.Fatalf("expected health status 200, got %d", minimalHealth.Code)
	}
	var minimal map[string]any
	if err := json.Unmarshal(minimalHealth.Body.Bytes(), &minimal); err != nil {
		t.Fatal(err)
	}
	if minimal["success"] != true {
		t.Fatalf("expected minimal health success: %v", minimal)
	}
	data, _ := minimal["data"].(map[string]any)
	if _, ok := data["running"]; !ok {
		t.Fatalf("expected minimal health running field: %v", minimal)
	}
	if _, ok := data["ready"]; ok {
		t.Fatalf("unauthorized health should not expose full status: %v", minimal)
	}

	fullHealth := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	router.ServeHTTP(fullHealth, req)
	if fullHealth.Code != http.StatusOK {
		t.Fatalf("expected authorized health status 200, got %d", fullHealth.Code)
	}
	var full map[string]any
	if err := json.Unmarshal(fullHealth.Body.Bytes(), &full); err != nil {
		t.Fatal(err)
	}
	if full["success"] != true {
		t.Fatalf("expected authorized health success: %v", full)
	}
	data, _ = full["data"].(map[string]any)
	if _, ok := data["ready"]; !ok {
		t.Fatalf("authorized health should expose full status: %v", full)
	}
}

func TestRPCUnifiedResponse(t *testing.T) {
	router := NewRouter(NewAppState("test-token"))

	success := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/rpc", strings.NewReader(`{"id":"req-1","command":"doctor","params":{}}`))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(success, req)
	if success.Code != http.StatusOK {
		t.Fatalf("expected success status 200, got %d", success.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(success.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["id"] != "req-1" || body["success"] != true {
		t.Fatalf("expected unified success response: %v", body)
	}
	if _, ok := body["data"].(map[string]any); !ok {
		t.Fatalf("expected data object: %v", body)
	}
	if _, ok := body["ok"]; ok {
		t.Fatalf("response should not contain ok: %v", body)
	}
	if _, ok := body["version"]; ok {
		t.Fatalf("response should not contain version: %v", body)
	}

	failure := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/v1/rpc", strings.NewReader(`{"id":"req-2","command":"missing.command","params":{}}`))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(failure, req)
	var failed map[string]any
	if err := json.Unmarshal(failure.Body.Bytes(), &failed); err != nil {
		t.Fatal(err)
	}
	if failed["id"] != "req-2" || failed["success"] != false {
		t.Fatalf("expected unified failure response: %v", failed)
	}
	errBody, _ := failed["error"].(map[string]any)
	if errBody["code"] != "unknown_command" || errBody["message"] == "" {
		t.Fatalf("expected error object: %v", failed)
	}
}
