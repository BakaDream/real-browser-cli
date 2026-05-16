package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/bakadream/real-browser-cli/internal/runtime"
)

func RunDaemon() error {
	paths, cfg, err := runtime.EnsureConfig()
	if err != nil {
		return err
	}
	cfg, _, err = runtime.EnsurePluginReleased(paths, cfg)
	if err != nil {
		return err
	}
	state := NewAppState(cfg.Token)
	apiServer := &http.Server{
		Addr:    fmt.Sprintf("%s:%s", host, apiPort),
		Handler: NewRouter(state),
	}
	wsServer := &http.Server{
		Addr:    fmt.Sprintf("%s:%s", host, wsPort),
		Handler: NewWSHandler(state),
	}
	apiListener, err := net.Listen("tcp", apiServer.Addr)
	if err != nil {
		return fmt.Errorf("API 端口启动失败 %s: %w", apiServer.Addr, err)
	}
	wsListener, err := net.Listen("tcp", wsServer.Addr)
	if err != nil {
		_ = apiListener.Close()
		return fmt.Errorf("WebSocket 端口启动失败 %s: %w", wsServer.Addr, err)
	}

	errCh := make(chan error, 2)
	go func() {
		if err := wsServer.Serve(wsListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	go MonitorIdleShutdown(state)

	go func() {
		<-state.Shutdown
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = wsServer.Shutdown(ctx)
		_ = apiServer.Shutdown(ctx)
	}()

	fmt.Printf("real-browser-cli server listening on http://%s:%s\n", host, apiPort)
	if err := apiServer.Serve(apiListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}
