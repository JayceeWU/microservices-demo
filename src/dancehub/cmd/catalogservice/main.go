package main

import (
	"log"

	catalogv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/catalog/v1"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/catalog/application"
	postgresadapter "github.com/JayceeWU/microservices-demo/dancehub/internal/catalog/infrastructure/postgres"
	grpctransport "github.com/JayceeWU/microservices-demo/dancehub/internal/catalog/transport/grpc"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
	"google.golang.org/grpc"
)

func main() {
	db := platform.OpenDatabase()
	defer db.Close()

	service := application.NewService(
		platform.NewUnitOfWork[application.ActorContext, application.Repositories](db, postgresadapter.NewRepositories),
	)
	server := grpctransport.NewServer(service)

	log.Fatal(platform.ServeGRPCAndHealth("catalogservice", func(grpcServer *grpc.Server) {
		catalogv1.RegisterCatalogServiceServer(grpcServer, server)
	}, db.Ping))
}
