package raftrpc

import (
	"fmt"
	"log"

	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	host = "localhost"
)

func MakeRpcClient(port int) RaftClient {
	address := fmt.Sprintf("%s:%d", host, port)
	fmt.Printf("Starting client, connecting to address %s\n", address)
	conn, err := grpc.Dial(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Client did not connect: %v", err)
	}
	// defer conn.Close()
	return NewRaftClient(conn)
}
