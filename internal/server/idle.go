package server

import (
	"fmt"
	"time"
)

func MonitorIdleShutdown(state *AppState) {
	ticker := time.NewTicker(idleShutdownCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			idleFor := state.IdleFor()
			if idleFor >= idleShutdownTTL {
				fmt.Printf("real-browser-cli daemon idle for %ds, shutting down\n", int(idleFor.Seconds()))
				state.RequestShutdown()
				return
			}
		case <-state.Shutdown:
			return
		}
	}
}
