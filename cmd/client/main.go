package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"raft/conf"
	"raft/raftrpc"
	"raft/utils"
	"strings"
)

func main() {
	flag.Parse()
	leaderNode := conf.Nodes[0] // for now just assume that node 0 is the leader
	grpcClient := raftrpc.MakeRpcClient(leaderNode.NodeId, leaderNode.Port)
	runInputLoop(grpcClient)
}

func runInputLoop(grpcClient *raftrpc.RpcClient) {
	stdinReader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("> ")
		msg := utils.MustSucceed(stdinReader.ReadString('\n'))
		msg = strings.TrimSpace(msg) + "\n"
		resp := grpcClient.SendClientAppend(msg)
		log.Printf("Response: %v (err was %v)\n", resp.Result, resp.Error)
	}
}
