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

func main() {
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

	store, err := scheduler.NewBadgerStore("data", historyTTL)
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
	mux.HandleFunc("DELETE /tasks/{id}", handler.WithAuth(handler.DeleteTask))
	mux.HandleFunc("DELETE /tasks", handler.WithAuth(handler.DeleteTasks))

	addr := ":" + *port
	srv := &http.Server{Addr: addr, Handler: mux}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	go r.Start(ctx)

	go func() {
		log.Printf("Listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()
	log.Println("Shutting down...")
	srv.Shutdown(context.Background())
}
