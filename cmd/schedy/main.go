package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/ksamirdev/schedy/internal/api"
	"github.com/ksamirdev/schedy/internal/executor"
	"github.com/ksamirdev/schedy/internal/logging"
	"github.com/ksamirdev/schedy/internal/runner"
	"github.com/ksamirdev/schedy/internal/scheduler"
	"github.com/ksamirdev/schedy/internal/version"
)

// dataDir is where BadgerDB persists tasks, from SCHEDY_DATA_DIR (default
// "data"). An env var rather than a flag so the server and the offline restore
// subcommand resolve the same directory the same way, and a restore can't
// target the wrong one.
func dataDir() string {
	if v := os.Getenv("SCHEDY_DATA_DIR"); v != "" {
		return v
	}
	return "data"
}

func main() {
	logging.Setup()

	// Offline subcommand: `schedy restore <backup-file>` loads a snapshot taken
	// via GET /admin/backup into an empty data dir, then exits.
	if len(os.Args) > 1 && os.Args[1] == "restore" {
		runRestore(os.Args[2:])
		return
	}

	port := flag.String("port", "8080", "port to listen on")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("schedy", version.String())
		return
	}

	// Terminal tasks are retained for history, then purged after this TTL.
	historyTTL := 72 * time.Hour
	if v := os.Getenv("SCHEDY_HISTORY_TTL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			slog.Error("invalid SCHEDY_HISTORY_TTL", "error", err)
			os.Exit(1)
		}
		historyTTL = d
	}

	store, err := scheduler.NewBadgerStore(dataDir(), historyTTL)
	if err != nil {
		slog.Error("open store", "error", err)
		os.Exit(1)
	}

	// Re-queue any tasks left mid-run by a previous crash/restart (at-least-once).
	if err := store.RecoverRunning(); err != nil {
		slog.Error("recover running tasks", "error", err)
	}

	exec := executor.NewExecutor()
	r := runner.New(store, exec, 10*time.Second)
	handler := api.New(store)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handler.Health)
	mux.HandleFunc("GET /readyz", handler.Ready)
	// Behind the API key: queue depth and backlog are operational detail, and a
	// Prometheus scrape config can carry the header.
	mux.HandleFunc("GET /metrics", handler.WithAuth(handler.Metrics))
	mux.HandleFunc("POST /tasks", handler.WithAuth(handler.CreateTask))
	mux.HandleFunc("GET /tasks", handler.WithAuth(handler.ListTasks))
	mux.HandleFunc("GET /tasks/{id}", handler.WithAuth(handler.GetTask))
	mux.HandleFunc("PUT /tasks/{id}", handler.WithAuth(handler.UpdateTask))
	mux.HandleFunc("POST /tasks/{id}/run", handler.WithAuth(handler.ReplayTask))
	mux.HandleFunc("DELETE /tasks/{id}", handler.WithAuth(handler.DeleteTask))
	mux.HandleFunc("DELETE /tasks", handler.WithAuth(handler.DeleteTasks))
	// Online snapshot of the whole store, behind the API key. Streamed, so a
	// mid-stream failure can only truncate the download (logged), not corrupt
	// anything; restore validates the file offline.
	mux.HandleFunc("GET /admin/backup", handler.WithAuth(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="schedy-backup.badger"`)
		if err := store.Backup(w); err != nil {
			slog.Error("backup", "error", err)
		}
	}))

	addr := ":" + *port
	srv := &http.Server{Addr: addr, Handler: mux}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	go r.Start(ctx)

	// Reclaim BadgerDB value-log garbage periodically. Signal when the loop has
	// observed cancellation so shutdown can close the store without racing an
	// in-flight GC pass.
	gcDone := make(chan struct{})
	go func() {
		store.RunGC(ctx, 10*time.Minute)
		close(gcDone)
	}()

	go func() {
		slog.Info("listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("serve", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")
	// Bounded drain: a background context would wait forever on one hung
	// connection, turning SIGINT into a process that never exits.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown", "error", err)
	}
	cancel()
	<-gcDone
	if err := store.Close(); err != nil {
		slog.Error("close store", "error", err)
	}
}

// runRestore loads a backup file into the (empty) data directory and exits. It
// refuses a non-empty dir, so it can't half-overwrite a live store.
func runRestore(args []string) {
	if len(args) != 1 {
		slog.Error("usage: schedy restore <backup-file>")
		os.Exit(1)
	}
	if err := scheduler.Restore(dataDir(), args[0]); err != nil {
		slog.Error("restore", "error", err)
		os.Exit(1)
	}
	slog.Info("restore complete", "from", args[0], "to", dataDir()+"/")
}
