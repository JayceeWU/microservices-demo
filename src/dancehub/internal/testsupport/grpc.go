package testsupport

import (
	"context"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type Cleanup interface{ Cleanup(func()) }

func GRPCConnection(test Cleanup, options []grpc.ServerOption, register func(*grpc.Server)) *grpc.ClientConn {
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer(options...)
	register(server)
	go func() { _ = server.Serve(listener) }()
	connection, err := grpc.NewClient(
		"passthrough:///bufconn",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
	)
	if err != nil {
		server.Stop()
		panic(err)
	}
	test.Cleanup(func() {
		_ = connection.Close()
		server.Stop()
		_ = listener.Close()
	})
	return connection
}
