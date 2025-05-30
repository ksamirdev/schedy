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

	store, err := scheduler.NewBadgerStore("data")
	if err != nil {
		log.Fatal(err)
	}
	exec := executor.NewExecutor()
	r := runner.New(store, exec, 10*time.Second)
	handler := api.New(store)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /tasks", handler.WithAuth(handler.CreateTask))
	mux.HandleFunc("GET /tasks", handler.WithAuth(handler.ListTasks))

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
