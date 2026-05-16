package runtime

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"time"
)

type Config struct {
	Token              string `json:"token"`
	CreatedAt          string `json:"created_at"`
	UpdatedAt          string `json:"updated_at"`
	PluginTemplateHash string `json:"plugin_template_hash,omitempty"`
	PluginReleasedAt   string `json:"plugin_released_at,omitempty"`
}

func EnsureConfig() (Paths, Config, error) {
	paths, err := Resolve()
	if err != nil {
		return Paths{}, Config{}, err
	}
	cfg, err := LoadConfig(paths)
	if err == nil && cfg.Token != "" {
		return paths, cfg, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Paths{}, Config{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	token, err := generateToken()
	if err != nil {
		return Paths{}, Config{}, err
	}
	cfg = Config{
		Token:     token,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := SaveConfig(paths, cfg); err != nil {
		return Paths{}, Config{}, err
	}
	return paths, cfg, nil
}

func LoadConfig(paths Paths) (Config, error) {
	data, err := os.ReadFile(paths.Config)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func SaveConfig(paths Paths, cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(paths.Config, data, 0o600)
}

func RotateToken() (Paths, Config, error) {
	paths, cfg, err := EnsureConfig()
	if err != nil {
		return Paths{}, Config{}, err
	}
	token, err := generateToken()
	if err != nil {
		return Paths{}, Config{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if cfg.CreatedAt == "" {
		cfg.CreatedAt = now
	}
	cfg.Token = token
	cfg.UpdatedAt = now
	cfg.PluginTemplateHash = ""
	cfg.PluginReleasedAt = ""
	if err := SaveConfig(paths, cfg); err != nil {
		return Paths{}, Config{}, err
	}
	return paths, cfg, nil
}

func generateToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}
