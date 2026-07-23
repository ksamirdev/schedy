package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/ksamirdev/schedy/internal/api"
	"github.com/ksamirdev/schedy/internal/executor"
	"github.com/ksamirdev/schedy/internal/runner"
	"github.com/ksamirdev/schedy/internal/scheduler"
)

// dataDir is where BadgerDB persists tasks. Shared by the server and the
// offline restore path so a restore can't target the wrong directory.
const dataDir = "data"

func main() {
	// Offline subcommand: `schedy restore <backup-file>` loads a snapshot taken
	// via GET /admin/backup into an empty data dir, then exits.
	if len(os.Args) > 1 && os.Args[1] == "restore" {
		runRestore(os.Args[2:])
		return
	}

	port := flag.String("port", "8080", "port to listen on")
	flag.Parse()

	// Terminal tasks are retained for history, then purged after this TTL.
	historyTTL := 72 * time.Hour
	if v := os.Getenv("SCHEDY_HISTORY_TTL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			log.Fatalf("invalid SCHEDY_HISTORY_TTL: %v", err)
		}
		historyTTL = d
	}

	store, err := scheduler.NewBadgerStore(dataDir, historyTTL)
	if err != nil {
		log.Fatal(err)
	}

	// Re-queue any tasks left mid-run by a previous crash/restart (at-least-once).
	if err := store.RecoverRunning(); err != nil {
		log.Printf("recover running tasks: %v", err)
	}

	exec := executor.NewExecutor()
	r := runner.New(store, exec, 10*time.Second)
	handler := api.New(store)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handler.Health)
	mux.HandleFunc("GET /readyz", handler.Ready)
	mux.HandleFunc("POST /tasks", handler.WithAuth(handler.CreateTask))
	mux.HandleFunc("GET /tasks", handler.WithAuth(handler.ListTasks))
	mux.HandleFunc("GET /tasks/{id}", handler.WithAuth(handler.GetTask))
	mux.HandleFunc("PUT /tasks/{id}", handler.WithAuth(handler.UpdateTask))
	mux.HandleFunc("DELETE /tasks/{id}", handler.WithAuth(handler.DeleteTask))
	mux.HandleFunc("DELETE /tasks", handler.WithAuth(handler.DeleteTasks))
	// Online snapshot of the whole store, behind the API key. Streamed, so a
	// mid-stream failure can only truncate the download (logged), not corrupt
	// anything; restore validates the file offline.
	mux.HandleFunc("GET /admin/backup", handler.WithAuth(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="schedy-backup.badger"`)
		if err := store.Backup(w); err != nil {
			log.Printf("backup: %v", err)
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
		log.Printf("Listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()
	log.Println("Shutting down...")
	srv.Shutdown(context.Background())
	<-gcDone
	if err := store.Close(); err != nil {
		log.Printf("close store: %v", err)
	}
}

// runRestore loads a backup file into the (empty) data directory and exits. It
// refuses a non-empty dir, so it can't half-overwrite a live store.
func runRestore(args []string) {
	if len(args) != 1 {
		log.Fatal("usage: schedy restore <backup-file>")
	}
	if err := scheduler.Restore(dataDir, args[0]); err != nil {
		log.Fatalf("restore: %v", err)
	}
	log.Printf("restore complete: %s -> %s/", args[0], dataDir)
}
