package main

import (
	"log"

	accountv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/account/v1"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/account/application"
	postgresadapter "github.com/JayceeWU/microservices-demo/dancehub/internal/account/infrastructure/postgres"
	grpctransport "github.com/JayceeWU/microservices-demo/dancehub/internal/account/transport/grpc"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
	"google.golang.org/grpc"
)

func main() {
	db := platform.OpenDatabase()
	defer db.Close()

	service := application.NewService(
		platform.NewUnitOfWork[application.ActorContext, application.Repositories](db, postgresadapter.NewRepositories),
		application.SystemClock{},
	)
	server := grpctransport.NewServer(service)

	log.Fatal(platform.ServeGRPCAndHealth("accountservice", func(grpcServer *grpc.Server) {
		accountv1.RegisterAccountServiceServer(grpcServer, server)
	}, db.Ping))
}
