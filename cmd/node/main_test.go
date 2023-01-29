package main

import (
	"fmt"
	"raft/conf"
	"raft/raftrpc"
	"raft/utils"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFiveNodeFarm(t *testing.T) {
	// Starts five nodes, all in this test process
	nodes := utils.Map(conf.Nodes, startNode)
	client := raftrpc.MakeRpcClient(0, conf.Nodes[0].Port)

	t.Run("AppendEntriesBroadcast", func(t *testing.T) {
		// The is a blocking call. When it returns, a majority of nodes in the
		// cluster should have that item in their logs.
		// Also note that the nodes may not have started up by the time this
		// returns. That's fine -- the raft protocol should be able to handle it.
		resp := client.SendClientAppend("set x 10")
		assert.Equal(t, "ok", resp.Response)
		assert.Empty(t, resp.Error)

		// Expect at least three of five nodes to have it in their log by the time the client responds
		numSuccesses := 0
		for _, node := range nodes {
			entries := node.RaftState.GetAllEntries()
			if len(entries) >= 1 && entries[0] == "set x 10" {
				numSuccesses++
			}
		}
		assert.GreaterOrEqual(t, numSuccesses, 3)

		// Expect the leader to have applied the item to the state machine
		leader := *utils.Find(nodes, func(n *RaftNode) bool {
			return n.RaftState.IsLeader()
		})
		res := leader.RaftState.ApplicationSM.Apply("get x")
		assert.Equal(t, "ok 10", res)
	})
}
