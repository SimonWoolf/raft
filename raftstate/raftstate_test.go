package raftstate

import (
	"context"
	"fmt"
	"log"
	"raft/raftlog"
	"raft/raftrpc"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNoClientAppendWhenFollower(t *testing.T) {
	r := NewRaftState(make(chan OutboxMessage), []NodeId{})
	res, err := r.HandleClientLogAppend("item")
	assert.False(t, res)
	assert.NotNil(t, err)
}

func TestNoAppendWhenNewerTerm(t *testing.T) {
	r := NewRaftState(make(chan OutboxMessage), []NodeId{})
	r.currentTerm = 2
	assert.False(t, r.HandleAppendEntries(
		&raftrpc.AppendEntriesRequest{
			Term:      Term(1),
			PrevIndex: -1,
			PrevTerm:  Term(-1),
			Entries:   []*raftrpc.LogEntry{raftlog.MakeLogEntry(1, "some entry")},
		},
	))
}

// Checks which are only functions of the raftlog are tested more thoroughly in
// raftlog_test. Here we check one failure and one success case to check the
// integration.
func TestNoAppendWhenNoPreviousItem(t *testing.T) {
	r := NewRaftState(make(chan OutboxMessage), []NodeId{})
	assert.False(t, r.HandleAppendEntries(
		&raftrpc.AppendEntriesRequest{
			Term:      Term(1),
			PrevIndex: 3,
			PrevTerm:  Term(1),
			Entries:   []*raftrpc.LogEntry{raftlog.MakeLogEntry(1, "some entry")},
		},
	))
}

func TestAppendCanSucceed(t *testing.T) {
	r := NewRaftState(make(chan OutboxMessage), []NodeId{})
	assert.True(t, r.HandleAppendEntries(
		&raftrpc.AppendEntriesRequest{
			Term:      Term(1),
			PrevIndex: -1,
			PrevTerm:  Term(-1),
			Entries:   []*raftrpc.LogEntry{raftlog.MakeLogEntry(1, "some entry")},
		},
	))
}

func TestAppendResponseMajoritySuccess(t *testing.T) {
	r := setUpEmptyLeader(t)

	// mock a success response from the majority of servers
	// (2 others is a majority as we can count ourselves)
	go mockResponses(r, Index(-1), 2, 2, 1)

	// send it a request
	res, err := r.HandleClientLogAppend("item")
	assert.True(t, res)
	assert.Nil(t, err)
}

func TestAppendResponseMajorityFailure(t *testing.T) {
	r := setUpEmptyLeader(t)

	// mock a success response from the majority of other servers
	go mockResponses(r, Index(-1), 3, 1, 1)

	// send it a request
	res, err := r.HandleClientLogAppend("item")
	assert.False(t, res)
	assert.NotNil(t, err)
}

func setUpEmptyLeader(t *testing.T) *RaftState {
	broadcastChan := make(chan OutboxMessage, 10)
	r := NewRaftState(broadcastChan, []NodeId{})

	// put into the leader state
	r.statem.Fire(triggerElection)
	r.statem.Fire(winElection)
	state, err := r.statem.State(context.Background())
	assert.Nil(t, err)
	assert.Equal(t, stateLeader, state)

	return r
}

// Mock the requested number of successes or failures in the initial broadcast;
// ignore subsequent retries for specific nodes
func mockResponses(r *RaftState, prevIndex Index, numFailures int, numSuccesses int, numAppended int32) {
	for {
		msg := (<-r.BroadcastChan)
		method := msg.MsgType
		log.Printf("Got message on broadcast channel; method = %v", method)
		switch method {
		case AppendMsgType:
			if len(msg.Recipients) == 1 {
				// Ignore all except all-node broadcasts from the initial client req
				return
			}
			i := 0
			for ; i < numFailures; i++ {
				r.HandleAppendEntriesResponse(prevIndex, false, i, numAppended)
			}
			for ; i < numSuccesses+numFailures; i++ {
				r.HandleAppendEntriesResponse(prevIndex, true, i, numAppended)
			}

		default:
			panic(fmt.Sprintf("Unhandled message type %v", method))
		}
	}
}
