package main

import (
	"context"
	"log"

	catalogv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/catalog/v1"
	schedulingv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/scheduling/v1"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/creditclient"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/scheduling/application"
	grpcadapter "github.com/JayceeWU/microservices-demo/dancehub/internal/scheduling/infrastructure/grpc"
	postgresadapter "github.com/JayceeWU/microservices-demo/dancehub/internal/scheduling/infrastructure/postgres"
	grpctransport "github.com/JayceeWU/microservices-demo/dancehub/internal/scheduling/transport/grpc"
	"google.golang.org/grpc"
)

func main() {
	db := platform.OpenDatabase()
	defer db.Close()

	creditClient := creditclient.New(platform.MustEnv("CREDIT_SERVICE_ADDR"))
	defer creditClient.Close()
	catalogConnection, err := platform.DialGRPC(platform.MustEnv("CATALOG_SERVICE_ADDR"))
	if err != nil {
		log.Fatal(err)
	}
	defer catalogConnection.Close()

	service := application.NewService(
		platform.NewUnitOfWork[application.ActorContext, application.Repositories](db, postgresadapter.NewRepositories),
		grpcadapter.CreditAdapter{Client: creditClient},
		grpcadapter.CatalogAdapter{Client: catalogv1.NewCatalogServiceClient(catalogConnection)},
		application.SystemClock{},
	)
	server := grpctransport.NewServer(service)
	workerContext, cancelWorker := context.WithCancel(context.Background())
	defer cancelWorker()
	go (application.Worker{Service: service}).Run(workerContext)

	log.Fatal(platform.ServeGRPCAndHealth("schedulingservice", func(grpcServer *grpc.Server) {
		schedulingv1.RegisterSchedulingServiceServer(grpcServer, server)
	}, db.Ping))
}
