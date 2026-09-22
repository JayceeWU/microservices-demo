package main

import (
	"context"
	"log"

	catalogv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/catalog/v1"
	creditsv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/credits/v1"
	ordersv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/orders/v1"
	paymentsv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/payments/v1"
	schedulingv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/scheduling/v1"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/orders/application"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/orders/infrastructure/flashsale"
	grpcadapter "github.com/JayceeWU/microservices-demo/dancehub/internal/orders/infrastructure/grpc"
	postgresadapter "github.com/JayceeWU/microservices-demo/dancehub/internal/orders/infrastructure/postgres"
	grpctransport "github.com/JayceeWU/microservices-demo/dancehub/internal/orders/transport/grpc"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
	"google.golang.org/grpc"
)

func main() {
	db := platform.OpenDatabase()
	defer db.Close()

	catalogConnection := mustDial("catalog", platform.MustEnv("CATALOG_SERVICE_ADDR"))
	schedulingConnection := mustDial("scheduling", platform.MustEnv("SCHEDULING_SERVICE_ADDR"))
	creditConnection := mustDial("credit", platform.MustEnv("CREDIT_SERVICE_ADDR"))
	paymentConnection := mustDial("payment", platform.MustEnv("PAYMENT_SERVICE_ADDR"))
	defer catalogConnection.Close()
	defer schedulingConnection.Close()
	defer creditConnection.Close()
	defer paymentConnection.Close()

	broker, err := flashsale.New(platform.MustEnv("FLASHSALE_REDIS_ADDR"), platform.MustEnv("RABBITMQ_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer broker.Close()

	service := application.NewService(
		platform.NewUnitOfWork[application.ActorContext, application.Repositories](db, postgresadapter.NewRepositories),
		grpcadapter.CatalogAdapter{Client: catalogv1.NewCatalogServiceClient(catalogConnection)},
		grpcadapter.SchedulingAdapter{Client: schedulingv1.NewSchedulingServiceClient(schedulingConnection)},
		grpcadapter.CreditAdapter{Client: creditsv1.NewCreditServiceClient(creditConnection)},
		grpcadapter.PaymentAdapter{Client: paymentsv1.NewPaymentServiceClient(paymentConnection)},
		broker,
		application.SystemClock{},
	)
	workerContext, stopWorkers := context.WithCancel(context.Background())
	defer stopWorkers()
	go (application.FulfillmentWorker{Service: service}).Run(workerContext)
	broker.Start(workerContext, service.ProcessFlashSale)

	server := grpctransport.NewServer(service)
	log.Fatal(platform.ServeGRPCAndHealth("orderservice", func(grpcServer *grpc.Server) {
		ordersv1.RegisterOrderServiceServer(grpcServer, server)
	}, db.Ping))
}

func mustDial(name, address string) *grpc.ClientConn {
	connection, err := platform.DialGRPC(address)
	if err != nil {
		log.Fatalf("%s gRPC configuration failed: %v", name, err)
	}
	return connection
}
