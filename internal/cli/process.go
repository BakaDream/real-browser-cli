package cli

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/bakadream/real-browser-cli/internal/runtime"
	"github.com/gofrs/flock"
)

func ensureServer() error {
	if err := ensureLocalState(); err != nil {
		return err
	}
	if isAlive() {
		return nil
	}
	return startServer()
}

func ensureLocalState() error {
	paths, cfg, err := runtime.EnsureConfig()
	if err != nil {
		return err
	}
	_, _, err = runtime.EnsurePluginReleased(paths, cfg)
	return err
}

func startServer() error {
	paths, cfg, err := runtime.EnsureConfig()
	if err != nil {
		return err
	}
	if _, _, err := runtime.EnsurePluginReleased(paths, cfg); err != nil {
		return err
	}
	lock := flock.New(paths.Lock)
	locked, err := lock.TryLock()
	if err != nil {
		return err
	}
	if !locked {
		time.Sleep(500 * time.Millisecond)
		if isAlive() {
			return nil
		}
		return fmt.Errorf("另一个 real-browser 启动流程正在进行")
	}
	defer lock.Unlock()
	if isAlive() {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	logFile, err := os.OpenFile(paths.Log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "daemon", "run")
	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	configureDaemonProcess(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = os.WriteFile(paths.PID, []byte(fmt.Sprintf("%d\n", cmd.Process.Pid)), 0o644)
	if err := cmd.Process.Release(); err != nil {
		return err
	}
	return waitServerStarted(15 * time.Second)
}

func waitServerStarted(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if isAlive() {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("real-browser server 启动超时，查看 ~/.real-browser-cli/real-browser.log")
}

func waitServerStopped(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !isAlive() {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return !isAlive()
}
