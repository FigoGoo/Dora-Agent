package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/FigoGoo/Dora-Agent/services/business/internal/bootstrap"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load business config: %v", err)
	}
	app, err := bootstrap.New(cfg)
	if err != nil {
		log.Fatalf("bootstrap business service: %v", err)
	}

	if app.HTTPServer != nil {
		go func() {
			app.Logger.Info("business_http_starting", "addr", cfg.HTTPAddr)
			if err := app.HTTPServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				app.Logger.Error("business_http_stopped", "error", err)
				os.Exit(1)
			}
		}()
	}
	go func() {
		app.Logger.Info("business_kitex_starting", "addr", cfg.KitexAddr, "registry", cfg.KitexRegistry)
		if err := app.Kitex.Run(); err != nil {
			app.Logger.Error("business_kitex_stopped", "error", err)
			os.Exit(1)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	if app.HTTPServer != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := app.HTTPServer.Shutdown(shutdownCtx); err != nil {
			app.Logger.Error("business_http_shutdown_failed", "error", err)
		}
		cancel()
	}
	if err := app.Kitex.Stop(); err != nil {
		app.Logger.Error("business_kitex_shutdown_failed", "error", err)
	}
	app.Logger.Info("business_shutdown_complete")
}
