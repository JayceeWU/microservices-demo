package main

import (
	"context"
	"log"
	"strings"

	accountv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/account/v1"
	catalogv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/catalog/v1"
	chatv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/chat/v1"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/chat/application"
	accountadapter "github.com/JayceeWU/microservices-demo/dancehub/internal/chat/infrastructure/account"
	catalogadapter "github.com/JayceeWU/microservices-demo/dancehub/internal/chat/infrastructure/catalog"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/chat/infrastructure/ids"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/chat/infrastructure/media"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/chat/infrastructure/objectstore"
	postgresadapter "github.com/JayceeWU/microservices-demo/dancehub/internal/chat/infrastructure/postgres"
	realtimeadapter "github.com/JayceeWU/microservices-demo/dancehub/internal/chat/infrastructure/realtime"
	redisadapter "github.com/JayceeWU/microservices-demo/dancehub/internal/chat/infrastructure/redis"
	grpctransport "github.com/JayceeWU/microservices-demo/dancehub/internal/chat/transport/grpc"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rabbitmq/amqp091-go"
	"google.golang.org/grpc"
)

func main() {
	db := platform.OpenDatabase()
	defer db.Close()
	relayDB, err := pgxpool.New(context.Background(), platform.MustEnv("CHAT_RELAY_DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer relayDB.Close()
	redisBus := redisadapter.New(platform.MustEnv("CHAT_REDIS_ADDR"))
	defer redisBus.Close()
	accountConnection, err := platform.DialGRPC(platform.MustEnv("ACCOUNT_SERVICE_ADDR"))
	if err != nil {
		log.Fatal(err)
	}
	defer accountConnection.Close()
	catalogConnection, err := platform.DialGRPC(platform.MustEnv("CATALOG_SERVICE_ADDR"))
	if err != nil {
		log.Fatal(err)
	}
	defer catalogConnection.Close()
	rabbitConnection, err := amqp091.Dial(platform.MustEnv("RABBITMQ_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer rabbitConnection.Close()
	mediaQueue, err := media.New(rabbitConnection)
	if err != nil {
		log.Fatal(err)
	}
	defer mediaQueue.Close()
	store, err := objectstore.New(platform.MustEnv("OBJECT_STORAGE_ENDPOINT"), platform.MustEnv("OBJECT_STORAGE_PUBLIC_ENDPOINT"), platform.MustEnv("OBJECT_STORAGE_ACCESS_KEY"), platform.MustEnv("OBJECT_STORAGE_SECRET_KEY"), platform.MustEnv("CHAT_MEDIA_BUCKET"), strings.EqualFold(platform.MustEnv("OBJECT_STORAGE_SECURE"), "true"))
	if err != nil {
		log.Fatal(err)
	}
	if err = store.EnsureBucket(context.Background()); err != nil {
		log.Fatal(err)
	}
	service := application.NewService(postgresadapter.NewUnitOfWork(db), accountadapter.New(accountv1.NewAccountServiceClient(accountConnection)), catalogadapter.New(catalogv1.NewCatalogServiceClient(catalogConnection)), store, mediaQueue, redisBus, redisBus, application.SystemClock{}, ids.UUID{})
	workerContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	go (realtimeadapter.Relay{Pool: relayDB, Bus: redisBus}).Run(workerContext)
	log.Fatal(platform.ServeGRPCAndHealth("chatservice", func(server *grpc.Server) { chatv1.RegisterChatServiceServer(server, grpctransport.New(service)) }, func(ctx context.Context) error {
		if err := db.Ping(ctx); err != nil {
			return err
		}
		return redisBus.Ping(ctx)
	}))
}
