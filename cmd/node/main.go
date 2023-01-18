package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"raft/conf"
	"raft/raftrpc"
	"raft/raftstate"
	"raft/utils"

	"google.golang.org/grpc"
)

type NodeId = int

var (
	host   = "localhost"
	nodeId = flag.Int("nodeId", 0, "The node id")
)

type RaftNode struct {
	raftrpc.UnimplementedRaftServer
	GrpcServer  *grpc.Server
	RaftState   *raftstate.RaftState
	PeerClients map[NodeId]raftrpc.RaftClient
}

func main() {
	flag.Parse()
	node := utils.Find(conf.Nodes, func(n conf.Node) bool {
		return n.NodeId == *nodeId
	})
	if node == nil {
		log.Fatalf("Unable to match nodeId %d in conf\n", *nodeId)
	}
	port := node.Port
	address := fmt.Sprintf("%s:%d", host, port)
	sock := utils.MustSucceed(net.Listen("tcp", address))

	broadcastChannel := make(chan raftstate.ProtobufMessage, 10)
	raftState := raftstate.NewRaftState(broadcastChannel)

	// TODO: once we have consensus working, can drop this startLeader stuff
	if node.StartLeader {
		raftState.BecomeLeader()
	}

	grpcServer := grpc.NewServer()

	peerClients := make(map[NodeId]raftrpc.RaftClient)
	for _, node := range conf.Nodes {
		peerClients[node.NodeId] = raftrpc.MakeRpcClient(node.Port)
	}

	raftNode := &RaftNode{
		GrpcServer:  grpcServer,
		RaftState:   raftState,
		PeerClients: peerClients,
	}
	raftrpc.RegisterRaftServer(grpcServer, raftNode)

	go raftNode.monitorBroadcastChannel()

	log.Printf("GRPC server started, listening at %v", sock.Addr())
	grpcServer.Serve(sock) // blocks while the server is running

}

func (r *RaftNode) monitorBroadcastChannel() {
	for {
		msg := <-r.RaftState.BroadcastChan
		method := msg.ProtoReflect().Descriptor().Name()
		log.Printf("Got message on broadcast channel; method = %s", method)
		switch method {
		case "AppendEntriesRequest":
			// TODO
			for _, client := range r.PeerClients {
				// resp, err := client.AppendEntriesRequest(ctx, &raftrpc.ClientLogAppendRequest{Item: "test item"})
			}

		default:
			panic(fmt.Sprintf("Unhandled message type %v", method))
		}

	}
}

func (r *RaftNode) AppendEntries(ctx context.Context, req *raftrpc.AppendEntriesRequest) (*raftrpc.BoolResponse, error) {
	// TODO do we want to include a match index here? nah we can just get it from the req since grpc matches up requests and responses
	result := r.RaftState.HandleAppendEntries(req)
	return &raftrpc.BoolResponse{Result: result}, nil
}

func (r *RaftNode) ClientLogAppend(ctx context.Context, req *raftrpc.ClientLogAppendRequest) (*raftrpc.MaybeErrorResponse, error) {
	log.Printf("Server received client log append req")
	// This is blocking, but no need to spawn off a goroutine cause each grpc req
	// is already handled in one
	result, error := r.RaftState.HandleClientLogAppend(req.Item)
	if error == nil {
		// Is this the point here that we actually apply the command to the KV store, as a leader? what about as a follower?
		return &raftrpc.MaybeErrorResponse{Result: result, Error: ""}, nil
	} else {
		return &raftrpc.MaybeErrorResponse{Result: result, Error: error.Error()}, nil
	}
}
