package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/term"

	"xiaozhang/internal/bootstrap"
	"xiaozhang/internal/config"
	"xiaozhang/internal/httpapi"
	"xiaozhang/internal/storage"
)

var version = "0.0.0-dev"

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "init-admin":
			cmdInitAdmin(os.Args[2:])
			return
		case "reset-password":
			cmdResetPassword(os.Args[2:])
			return
		case "serve":
			cmdServe(os.Args[2:])
			return
		}
	}
	cmdServe(os.Args[1:])
}

// openDBFull opens the database and applies pending migrations.
func openDBFull(dataDir string) (*sql.DB, error) {
	db, err := storage.Open(dataDir)
	if err != nil {
		return nil, err
	}
	if err := storage.Migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// promptSecret reads a password without echoing it.
func promptSecret(prompt string) string {
	fmt.Fprint(os.Stderr, prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		log.Fatalf("read password: %v", err)
	}
	return string(b)
}

// cmdInitAdmin creates the first administrator locally (server shell
// required). Password comes from XIAOZHANG_ADMIN_PASSWORD or an
// interactive prompt — never from a CLI argument that lands in history.
func cmdInitAdmin(args []string) {
	fs := flag.NewFlagSet("init-admin", flag.ExitOnError)
	data := fs.String("data", config.Getenv("XIAOZHANG_DATA_DIR", "./data"), "data directory")
	username := fs.String("username", "", "admin username (required)")
	name := fs.String("name", "", "display name")
	ledgerName := fs.String("ledger", "家庭账本", "default ledger name")
	_ = fs.Parse(args)

	if *username == "" {
		log.Fatal("-username is required")
	}
	password := os.Getenv("XIAOZHANG_ADMIN_PASSWORD")
	if password == "" {
		password = promptSecret("Admin password (min 8 chars): ")
	}
	db, err := openDBFull(*data)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if err := bootstrap.InitAdmin(db, *username, *name, password, *ledgerName); err != nil {
		log.Fatalf("init admin: %v", err)
	}
	fmt.Println("administrator created; log in via the web UI")
}

// cmdResetPassword recovers access when the admin password is lost.
// Requires server-local execution; revokes existing sessions.
func cmdResetPassword(args []string) {
	fs := flag.NewFlagSet("reset-password", flag.ExitOnError)
	data := fs.String("data", config.Getenv("XIAOZHANG_DATA_DIR", "./data"), "data directory")
	username := fs.String("username", "", "username (required)")
	_ = fs.Parse(args)
	if *username == "" {
		log.Fatal("-username is required")
	}
	password := os.Getenv("XIAOZHANG_ADMIN_PASSWORD")
	if password == "" {
		password = promptSecret("New password (min 8 chars): ")
	}
	db, err := openDBFull(*data)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if err := bootstrap.ResetPassword(db, *username, password); err != nil {
		log.Fatalf("reset password: %v", err)
	}
	fmt.Println("password updated; previous sessions revoked")
}

func cmdServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", config.Getenv("XIAOZHANG_ADDR", "127.0.0.1:8787"), "listen address")
	data := fs.String("data", config.Getenv("XIAOZHANG_DATA_DIR", "./data"), "data directory")
	secureCookies := fs.Bool("secure-cookies", config.Getenv("XIAOZHANG_SECURE_COOKIES", "") == "1", "mark session cookies Secure (HTTPS)")
	_ = fs.Parse(args)

	cfg := config.Config{
		Addr:          *addr,
		DataDir:       *data,
		Version:       version,
		SecureCookies: *secureCookies,
	}

	db, err := openDBFull(cfg.DataDir)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	if needs, err := bootstrap.NeedsInit(db); err == nil && needs {
		log.Printf("no administrator yet; run: xiaozhang init-admin -data %s -username <name>", cfg.DataDir)
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
