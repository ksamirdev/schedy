package main

import (
	"context"
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
	store, err := scheduler.NewBadgerStore("data")
	if err != nil {
		log.Fatal(err)
	}
	exec := executor.NewExecutor()
	r := runner.New(store, exec, 10*time.Second)
	handler := api.New(store)

	mux := http.NewServeMux()
	mux.HandleFunc("/tasks", handler.CreateTask)

	srv := &http.Server{Addr: ":8080", Handler: mux}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	go r.Start(ctx)

	go func() {
		log.Println("Listening on :8080")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()
	log.Println("Shutting down...")
	srv.Shutdown(context.Background())
}
