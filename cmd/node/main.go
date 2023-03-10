package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"raft/conf"
	"raft/kvserver"
	"raft/raftrpc"
	"raft/raftstate"
	"raft/utils"

	"google.golang.org/grpc"
)

type NodeId = raftstate.NodeId

type RaftNode struct {
	raftrpc.UnimplementedRaftServer
	GrpcServer  *grpc.Server
	RaftState   *raftstate.RaftState
	Id          NodeId
	PeerClients map[NodeId]*raftrpc.RpcClient
}

func main() {
	nodeId := flag.Int("nodeId", 0, "The node id")
	flag.Parse()

	node := utils.Find(conf.Nodes, func(n conf.Node) bool {
		return n.NodeId == *nodeId
	})
	if node == nil {
		log.Fatalf("Unable to match nodeId %d in conf\n", *nodeId)
	}

	startNode(*node)

	// startNode starts the grpc server asynchronously. But now need to keep the
	// main goroutine alive or the process will die. So just use select to block
	// forever
	select {}
}

func startNode(myNode conf.Node) *RaftNode {
	port := myNode.Port
	address := fmt.Sprintf("localhost:%d", port)
	log.Printf("startNode: attempting to bind to %v", address)
	sock := utils.MustSucceed(net.Listen("tcp", address))
	otherNodes := utils.Filter(conf.Nodes, func(node conf.Node) bool { return node.NodeId != myNode.NodeId })
	otherNodeIds := utils.Map(otherNodes, func(node conf.Node) NodeId { return node.NodeId })

	broadcastChannel := make(chan raftstate.OutboxMessage, 10)
	// The actual application, which here is a simple key/value server. We pass
	// it the state-machine apply channel, and then forget about it. That's the
	// only point of communication, and it's one-way.
	applicationStateMachine := kvserver.NewKvServer()

	// raftState uses a goroutine to serialise edits to its internal state. Start
	// it and leave it running
	raftState := raftstate.NewRaftState(broadcastChannel, applicationStateMachine, otherNodeIds)
	go raftState.Run()

	// TODO: once we have consensus working, can drop this startLeader stuff
	if myNode.StartLeader {
		raftState.BecomeLeader()
	}

	grpcServer := grpc.NewServer()

	peerClients := make(map[NodeId]*raftrpc.RpcClient)
	for _, node := range otherNodes {
		peerClients[node.NodeId] = raftrpc.MakeRpcClient(node.NodeId, node.Port)
	}

	raftNode := &RaftNode{
		Id:          myNode.NodeId,
		GrpcServer:  grpcServer,
		RaftState:   raftState,
		PeerClients: peerClients,
	}
	raftrpc.RegisterRaftServer(grpcServer, raftNode)

	go raftNode.monitorBroadcastChannel()

	log.Printf("GRPC server started, listening at %v", sock.Addr())
	// this blocks while the server is running, so run it in a goroutine so that
	// we can return the RaftNode (mostly useful for testing)
	go grpcServer.Serve(sock)
	return raftNode

}

func (r *RaftNode) monitorBroadcastChannel() {
	for {
		msg := <-r.RaftState.BroadcastChan
		method := msg.MsgType
		switch method {
		case raftstate.AppendMsgType:
			log.Println("Got appendEntriesRequest from broadcast channel; msg =", msg)
			req := msg.Msg.(*raftrpc.AppendEntriesRequest)
			for _, nodeId := range msg.Recipients {
				client := r.PeerClients[nodeId]
				go func(client *raftrpc.RpcClient) {
					resp, err := client.SendAppendEntries(req)
					if err != nil {
						// Log a failure, and don't even bother to notify the raftstate,
						// which does not rely on getting appendentriesresponses from
						// everyone else in a reasonable amount of time, or indeed at all
						log.Printf("Error sending AppendEntries rpc to node %v; err = %v", client.Address, err)
					} else {
						r.RaftState.NoteEntryAppended(req.PrevIndex, resp.Result, resp.Term, client.NodeId, int32(len(req.Entries)))
					}
				}(client)
			}

		default:
			panic(fmt.Sprintf("Unhandled message type %v", method))
		}
	}
}

// Protobuf Raft service method implementations

func (r *RaftNode) AppendEntries(ctx context.Context, req *raftrpc.AppendEntriesRequest) (*raftrpc.AppendEntriesResponse, error) {
	log.Printf("Server received append entries req: %v\n", req)
	return r.RaftState.AppendEntries(req), nil
}

func (r *RaftNode) ClientLogAppend(ctx context.Context, req *raftrpc.ClientLogAppendRequest) (*raftrpc.ClientLogAppendResponse, error) {
	log.Printf("Server received client log append req: %v\n", req)
	return r.RaftState.ClientLogAppend(req), nil
}
