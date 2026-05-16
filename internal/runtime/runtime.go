package runtime

import (
	"os"
	"path/filepath"
)

const EnvHome = "REAL_BROWSER_CLI_HOME"

type Paths struct {
	Home      string
	Log       string
	Lock      string
	PID       string
	Config    string
	PluginDir string
}

func Resolve() (Paths, error) {
	home := os.Getenv(EnvHome)
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			home = ".real-browser-cli"
		} else {
			home = filepath.Join(userHome, ".real-browser-cli")
		}
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		return Paths{}, err
	}
	return Paths{
		Home:      home,
		Log:       filepath.Join(home, "real-browser.log"),
		Lock:      filepath.Join(home, "real-browser.lock"),
		PID:       filepath.Join(home, "real-browser.pid"),
		Config:    filepath.Join(home, "config.json"),
		PluginDir: filepath.Join(home, "browser-plugin"),
	}, nil
}
