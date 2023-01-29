package raftrpc

import (
	"context"
	"fmt"
	"log"
	"time"

	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	host = "localhost"
)

// thin wrapper around the actual rpc client
type RpcClient struct {
	client  RaftClient
	Address string
	NodeId  int
}

func MakeRpcClient(nodeId int, port int) *RpcClient {
	address := fmt.Sprintf("%s:%d", host, port)
	fmt.Printf("Starting client, connecting to address %s\n", address)
	conn, err := grpc.Dial(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Client did not connect: %v", err)
	}
	// defer conn.Close()
	return &RpcClient{
		client:  NewRaftClient(conn),
		Address: address,
		NodeId:  nodeId,
	}
}

func (r *RpcClient) SendClientAppend(msg string) *ClientLogAppendResponse {
	// retry network errors up to three times
	// TODO idempotency for client requests; request id?
	for i := 1; i <= 3; i++ {
		resp, err := r.sendClientAppendImpl(msg)
		if err == nil {
			return resp
		}
		log.Printf("Error performing sendClientAppend: %s (attempt %d)", err.Error(), i)
		time.Sleep(1 * time.Second)
	}
	log.Fatal("Unable to perform sendClientAppend; giving up")
	return nil
}

func (r *RpcClient) sendClientAppendImpl(msg string) (*ClientLogAppendResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return r.client.ClientLogAppend(ctx, &ClientLogAppendRequest{Item: msg})
}

func (r *RpcClient) SendAppendEntries(req *AppendEntriesRequest) (*AppendEntriesResponse, error) {
	ctx := context.Background()
	return r.client.AppendEntries(ctx, req)
}
