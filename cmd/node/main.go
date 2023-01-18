package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"raft/raftrpc"
	"raft/raftstate"

	"google.golang.org/grpc"
)

var (
	host = "localhost"
	port = flag.Int("port", 10001, "The server port")
)

type RaftNode struct {
	raftrpc.UnimplementedRaftServer
	GrpcServer *grpc.Server
	RaftState  *raftstate.RaftState
}

func main() {
	flag.Parse()
	address := fmt.Sprintf("%s:%d", host, *port)
	fmt.Println(address)
	sock, err := net.Listen("tcp", address)
	if err != nil {
		log.Fatalf("Error creating tcp server: %v\n", err)
	}

	broadcastChannel := make(chan raftstate.ProtobufMessage, 10)
	raftState := raftstate.NewRaftState(broadcastChannel)

	// TODO listen on broadcast channel in a goroutine and handle

	grpcServer := grpc.NewServer()
	raftNode := &RaftNode{
		GrpcServer: grpcServer,
		RaftState:  raftState,
	}
	raftrpc.RegisterRaftServer(grpcServer, raftNode)
	if err := grpcServer.Serve(sock); err != nil {
		log.Fatalf("failed to start GRPC server: %v", err)
	}
	log.Printf("GRPC server listening at %v", sock.Addr())
}

func (r *RaftNode) AppendEntries(ctx context.Context, req *raftrpc.AppendEntriesRequest) (*raftrpc.BoolResponse, error) {
	result := r.RaftState.HandleAppendEntries(req)
	return &raftrpc.BoolResponse{Result: result}, nil
}

func (r *RaftNode) ClientLogAppend(ctx context.Context, req *raftrpc.ClientLogAppendRequest) (*raftrpc.MaybeErrorResponse, error) {
	result, error := r.RaftState.HandleClientLogAppend(req.Item)
	if error == nil {
		return &raftrpc.MaybeErrorResponse{Result: result, Error: ""}, nil
	} else {
		return &raftrpc.MaybeErrorResponse{Result: result, Error: error.Error()}, nil
	}
}
