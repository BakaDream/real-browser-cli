package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/bakadream/real-browser-cli/internal/protocol"
	"github.com/bakadream/real-browser-cli/internal/runtime"
	"github.com/google/uuid"
)

const apiBaseURL = "http://127.0.0.1:18767"

var output io.Writer = os.Stdout

func request(method, path string, payload any, timeout time.Duration) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, apiBaseURL+path, body)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token := currentToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return data, newHTTPError(resp.StatusCode, data)
	}
	return data, nil
}

type httpError struct {
	StatusCode int
	Body       []byte
	Code       string
	Message    string
}

func (e httpError) Error() string {
	if e.Code != "" || e.Message != "" {
		return formatError(e.Code, e.Message)
	}
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, string(e.Body))
}

func newHTTPError(statusCode int, body []byte) error {
	var resp apiResponse
	if err := json.Unmarshal(body, &resp); err == nil && resp.Error != nil {
		return httpError{StatusCode: statusCode, Body: body, Code: resp.Error.Code, Message: resp.Error.Message}
	}
	return httpError{StatusCode: statusCode, Body: body}
}

func rpc(command string, tab string, params map[string]any, timeout time.Duration, jsonOut bool, debugIDs bool) ([]byte, error) {
	if params == nil {
		params = map[string]any{}
	}
	data, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	target := map[string]any{}
	if tab != "" {
		target["tab"] = tab
	}
	return request(http.MethodPost, "/v1/rpc", protocol.APIRequest{
		ID:      uuid.NewString(),
		Command: command,
		Target:  target,
		Params:  data,
		Options: protocol.RequestOptions{
			TimeoutMS: int64(timeout / time.Millisecond),
			JSON:      jsonOut,
			DebugIDs:  debugIDs,
		},
	}, timeout+5*time.Second)
}

func currentToken() string {
	paths, err := runtime.Resolve()
	if err != nil {
		return ""
	}
	cfg, err := runtime.LoadConfig(paths)
	if err != nil {
		return ""
	}
	return cfg.Token
}

func printRaw(data []byte) error {
	var value any
	if err := json.Unmarshal(data, &value); err == nil {
		buf := &bytes.Buffer{}
		enc := json.NewEncoder(buf)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		if err := enc.Encode(value); err == nil {
			_, err := fmt.Fprint(output, buf.String())
			return err
		}
	}
	_, err := fmt.Fprintln(output, string(data))
	return err
}

func printJSON(value any) error {
	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return err
	}
	_, err := fmt.Fprint(output, buf.String())
	return err
}

func isAlive() bool {
	data, err := request(http.MethodGet, "/health", nil, time.Second)
	if err != nil {
		return false
	}
	resp, err := parseAPIResponse(data)
	if err == nil {
		if m, ok := resp.DataMap(); ok {
			running, _ := m["running"].(bool)
			return running
		}
	}
	return false
}
