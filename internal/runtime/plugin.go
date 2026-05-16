package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	browserplugin "github.com/bakadream/real-browser-cli/browser-plugin"
)

func EnsurePluginReleased(paths Paths, cfg Config) (Config, bool, error) {
	hash, err := PluginTemplateHash()
	if err != nil {
		return cfg, false, err
	}
	if cfg.PluginTemplateHash == hash && cfg.PluginReleasedAt != "" {
		configData, configErr := os.ReadFile(filepath.Join(paths.PluginDir, "config.js"))
		_, manifestErr := os.Stat(filepath.Join(paths.PluginDir, "manifest.json"))
		if manifestErr == nil && configErr == nil && strings.Contains(string(configData), cfg.Token) {
			return cfg, false, nil
		}
	}
	if err := ReleasePlugin(paths, cfg.Token); err != nil {
		return cfg, false, err
	}
	cfg.PluginTemplateHash = hash
	cfg.PluginReleasedAt = time.Now().UTC().Format(time.RFC3339)
	cfg.UpdatedAt = cfg.PluginReleasedAt
	if err := SaveConfig(paths, cfg); err != nil {
		return cfg, false, err
	}
	return cfg, true, nil
}

func ReleasePlugin(paths Paths, token string) error {
	if err := os.MkdirAll(paths.PluginDir, 0o755); err != nil {
		return err
	}
	for _, name := range browserplugin.FileNames {
		if name == "config.js" {
			continue
		}
		data, err := browserplugin.Files.ReadFile(name)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(paths.PluginDir, name), data, 0o644); err != nil {
			return err
		}
	}
	config := fmt.Sprintf(
		"const TID = '__agent_browser_cli_bridge_26c9f1';\n"+
			"globalThis.REAL_BROWSER_TOKEN = %q;\n"+
			"globalThis.REAL_BROWSER_WS_URL = 'ws://127.0.0.1:18765/?token=' + encodeURIComponent(globalThis.REAL_BROWSER_TOKEN);\n",
		token,
	)
	return os.WriteFile(filepath.Join(paths.PluginDir, "config.js"), []byte(config), 0o600)
}

func PluginTemplateHash() (string, error) {
	h := sha256.New()
	for _, name := range browserplugin.FileNames {
		data, err := browserplugin.Files.ReadFile(name)
		if err != nil {
			return "", err
		}
		h.Write([]byte(name))
		h.Write([]byte{0})
		h.Write(data)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func IsAuthToken(token string, cfg Config) bool {
	return token != "" && cfg.Token != "" && strings.TrimSpace(token) == cfg.Token
}
