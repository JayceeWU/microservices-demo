package main

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/chat/infrastructure/media"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/chat/infrastructure/objectstore"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rabbitmq/amqp091-go"
)

func main() {
	platform.ConfigureJSONLogging("chat-media-worker")
	shutdown := platform.InitTelemetry("chat-media-worker")
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdown(ctx)
	}()
	db, err := pgxpool.New(context.Background(), platform.MustEnv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	rabbit, err := amqp091.Dial(platform.MustEnv("RABBITMQ_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer rabbit.Close()
	topology, err := media.New(rabbit)
	if err != nil {
		log.Fatal(err)
	}
	defer topology.Close()
	store, err := objectstore.New(platform.MustEnv("OBJECT_STORAGE_ENDPOINT"), "", platform.MustEnv("OBJECT_STORAGE_ACCESS_KEY"), platform.MustEnv("OBJECT_STORAGE_SECRET_KEY"), platform.MustEnv("CHAT_MEDIA_BUCKET"), strings.EqualFold(platform.MustEnv("OBJECT_STORAGE_SECURE"), "true"))
	if err != nil {
		log.Fatal(err)
	}
	health := http.NewServeMux()
	health.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := db.Ping(ctx); err != nil || rabbit.IsClosed() {
			http.Error(w, "dependency unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	go func() {
		if err := http.ListenAndServe(":8080", health); err != nil {
			log.Printf("health server stopped: %v", err)
		}
	}()
	log.Fatal((media.Worker{Pool: db, Rabbit: rabbit, Objects: store, ClamAV: platform.MustEnv("CLAMAV_ADDR")}).Run(context.Background()))
}
