package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"xiaozhang/internal/config"
	"xiaozhang/internal/httpapi"
	"xiaozhang/internal/storage"
)

var version = "0.0.0-dev"

func main() {
	var (
		addr = flag.String("addr", config.Getenv("XIAOZHANG_ADDR", "127.0.0.1:8787"), "listen address")
		data = flag.String("data", config.Getenv("XIAOZHANG_DATA_DIR", "./data"), "data directory")
	)
	flag.Parse()

	cfg := config.Config{
		Addr:    *addr,
		DataDir: *data,
		Version: version,
	}

	db, err := storage.Open(cfg.DataDir)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	if err := storage.Migrate(db); err != nil {
		log.Fatalf("migrate database: %v", err)
	}

	srv := httpapi.NewServer(cfg, db)

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("xiaozhang %s listening on http://%s (data: %s)", cfg.Version, cfg.Addr, cfg.DataDir)
		errCh <- httpSrv.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Printf("received %s, shutting down", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(ctx); err != nil {
			log.Printf("shutdown error: %v", err)
		}
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}
}
