package cli

import (
	"encoding/json"
	"fmt"

	"github.com/bakadream/real-browser-cli/internal/protocol"
)

type daemonHealth struct {
	Running bool `json:"running"`
	Ready   bool `json:"ready"`
}

type apiResponse struct {
	ID      string             `json:"id,omitempty"`
	Success bool               `json:"success"`
	Data    json.RawMessage    `json:"data,omitempty"`
	Error   *protocol.APIError `json:"error,omitempty"`
	Meta    map[string]any     `json:"meta,omitempty"`
}

func parseAPIResponse(data []byte) (apiResponse, error) {
	var resp apiResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return apiResponse{}, err
	}
	return resp, nil
}

func (r apiResponse) DataMap() (map[string]any, bool) {
	if len(r.Data) == 0 {
		return nil, false
	}
	var m map[string]any
	if err := json.Unmarshal(r.Data, &m); err != nil {
		return nil, false
	}
	return m, true
}

func formatError(code string, message string) string {
	if code == "" {
		code = "error"
	}
	if message == "" {
		return code
	}
	return fmt.Sprintf("%s: %s", code, message)
}

func parseDaemonHealth(data []byte) (daemonHealth, error) {
	resp, err := parseAPIResponse(data)
	if err != nil {
		var health daemonHealth
		if rawErr := json.Unmarshal(data, &health); rawErr != nil {
			return daemonHealth{}, err
		}
		return health, nil
	}
	m, ok := resp.DataMap()
	if !ok {
		var health daemonHealth
		if rawErr := json.Unmarshal(data, &health); rawErr != nil {
			return daemonHealth{}, nil
		}
		return health, nil
	}
	health := daemonHealth{}
	health.Running, _ = m["running"].(bool)
	health.Ready, _ = m["ready"].(bool)
	return health, nil
}

func printDaemonLine(line string) error {
	_, err := fmt.Fprintln(output, line)
	return err
}

func printDaemonStart(alreadyStarted bool) error {
	if globals.JSON {
		return printJSON(map[string]any{"success": true, "data": map[string]any{"started": !alreadyStarted, "alreadyStarted": alreadyStarted}, "meta": map[string]any{"command": "daemon.start", "warnings": []string{}}})
	}
	if globals.Quiet {
		return nil
	}
	if alreadyStarted {
		return printDaemonLine("real browser daemon has already started.")
	}
	return printDaemonLine("real browser daemon start successfully.")
}

func printDaemonStartFailed() error {
	if globals.Quiet {
		return nil
	}
	return printDaemonLine("real browser daemon start failed.")
}

func printDaemonStatus(health daemonHealth) error {
	if globals.JSON {
		return printJSON(map[string]any{"success": true, "data": health, "meta": map[string]any{"command": "daemon.status", "warnings": []string{}}})
	}
	if globals.Quiet {
		if health.Running {
			return printDaemonLine("running")
		}
		return printDaemonLine("stopped")
	}
	if !health.Running {
		return printDaemonLine("real browser daemon is not running.")
	}
	if err := printDaemonLine("real browser daemon is running."); err != nil {
		return err
	}
	if health.Ready {
		return printDaemonLine("browser plugin is connected.")
	}
	return printDaemonLine("browser plugin is not connected.")
}

func printDaemonStopSuccess() error {
	if globals.JSON {
		return printJSON(map[string]any{"success": true, "data": map[string]any{"status": "stopped"}, "meta": map[string]any{"command": "daemon.stop", "warnings": []string{}}})
	}
	if globals.Quiet {
		return nil
	}
	return printDaemonLine("real browser daemon stop successfully.")
}

func printDaemonStopFailed() error {
	if globals.Quiet {
		return nil
	}
	return printDaemonLine("real browser daemon stop failed.")
}

func printDaemonRestartSuccess(health daemonHealth) error {
	if globals.JSON {
		return printJSON(map[string]any{"success": true, "data": map[string]any{"running": health.Running, "ready": health.Ready}, "meta": map[string]any{"command": "daemon.restart", "warnings": []string{}}})
	}
	if globals.Quiet {
		return nil
	}
	if err := printDaemonLine("real browser daemon restart successfully."); err != nil {
		return err
	}
	if health.Ready {
		return printDaemonLine("browser plugin is connected.")
	}
	return printDaemonLine("browser plugin is not connected.")
}

func printDaemonRestartFailed() error {
	if globals.Quiet {
		return nil
	}
	return printDaemonLine("real browser daemon restart failed.")
}
