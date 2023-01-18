package main

import (
	"context"
	"flag"
	"log"
	"raft/conf"
	"raft/raftrpc"
	"time"
)

func main() {
	flag.Parse()
	leaderNode := conf.Nodes[0] // for now just assume that node 0 is the leader
	grpcClient := raftrpc.MakeRpcClient(leaderNode.Port)

	// TODO interactive command loop from kvclient
	// Contact the server and print out its response.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := grpcClient.ClientLogAppend(ctx, &raftrpc.ClientLogAppendRequest{Item: "test item"})
	if err != nil {
		log.Fatalf("Error from server: %v", err)
	}
	log.Printf("Response: %v (err was %v)", resp.Result, resp.Error)
}
