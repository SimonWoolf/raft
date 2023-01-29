package raftstate

import (
	"fmt"
	"log"
	"raft/raftlog"
	"raft/raftrpc"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNoClientAppendWhenFollower(t *testing.T) {
	r := NewRaftState(make(chan OutboxMessage), MockAppSm{}, []NodeId{})
	_, err := r.HandleClientLogAppend("item")
	assert.NotNil(t, err)
}

func TestNoAppendWhenNewerTerm(t *testing.T) {
	r := NewRaftState(make(chan OutboxMessage), MockAppSm{}, []NodeId{})
	r.currentTerm = 2
	res, _ := r.HandleAppendEntries(
		&raftrpc.AppendEntriesRequest{
			Term:         Term(1),
			PrevIndex:    -1,
			PrevTerm:     Term(-1),
			Entries:      []*raftrpc.LogEntry{raftlog.MakeLogEntry(1, "some entry")},
			LeaderCommit: Index(-1),
		},
	)
	assert.False(t, res)
}

func TestNoAppendWhenLeader(t *testing.T) {
	r := NewRaftState(make(chan OutboxMessage), MockAppSm{}, []NodeId{})
	r.BecomeLeader()
	res, _ := r.HandleAppendEntries(
		&raftrpc.AppendEntriesRequest{
			Term:         Term(1),
			PrevIndex:    -1,
			PrevTerm:     Term(-1),
			Entries:      []*raftrpc.LogEntry{raftlog.MakeLogEntry(1, "some entry")},
			LeaderCommit: Index(-1),
		},
	)
	assert.False(t, res)
}

// Checks which are only functions of the raftlog are tested more thoroughly in
// raftlog_test. Here we check one failure and one success case to check the
// integration.
func TestNoAppendWhenNoPreviousItem(t *testing.T) {
	r := NewRaftState(make(chan OutboxMessage), MockAppSm{}, []NodeId{})
	res, _ := r.HandleAppendEntries(
		&raftrpc.AppendEntriesRequest{
			Term:         Term(1),
			PrevIndex:    3,
			PrevTerm:     Term(1),
			Entries:      []*raftrpc.LogEntry{raftlog.MakeLogEntry(1, "some entry")},
			LeaderCommit: Index(-1),
		},
	)
	assert.False(t, res)
}

func TestAppendCanSucceed(t *testing.T) {
	r := NewRaftState(make(chan OutboxMessage), MockAppSm{}, []NodeId{})
	// default to follower
	assert.True(t, r.IsFollower())
	res, _ := r.HandleAppendEntries(
		&raftrpc.AppendEntriesRequest{
			Term:         Term(1),
			PrevIndex:    -1,
			PrevTerm:     Term(-1),
			Entries:      []*raftrpc.LogEntry{raftlog.MakeLogEntry(1, "some entry")},
			LeaderCommit: Index(-1),
		},
	)
	assert.True(t, res)
}

func TestAppendResponseMajoritySuccess(t *testing.T) {
	r := setUpEmptyLeader(t)
	assert.EqualValues(t, -1, r.commitIndex)
	assert.EqualValues(t, -1, r.lastApplied)

	// mock a success response from the majority of servers
	// (2 others is a majority as we can count ourselves)
	go mockResponses(r, Index(-1), 2, 2, 1)

	// send it a request
	res, err := r.HandleClientLogAppend("item")
	assert.Equal(t, "success", res)
	assert.Nil(t, err)
	assert.EqualValues(t, 0, r.commitIndex)
	assert.EqualValues(t, 0, r.lastApplied)
}

// Commented out because after the 'no more per-client-req response list'
// refactor, a majority failure just causes the request to hang indefinitely
// while it keeps retrying, it never actually returns to the client. TODO think
// of a way to test this
// func TestAppendResponseMajorityFailure(t *testing.T) {
//   r := setUpEmptyLeader(t)

//   // mock a success response from the majority of other servers
//   go mockResponses(r, Index(-1), 3, 1, 1)

//   // send it a request
//   _, err := r.HandleClientLogAppend("item")
//   assert.NotNil(t, err)
//   assert.EqualValues(t, -1, r.commitIndex)
//   assert.EqualValues(t, -1, r.lastApplied)
// }

func TestDiscoverHigherTermAsLeaderFromAppend(t *testing.T) {
	r := NewRaftState(make(chan OutboxMessage), MockAppSm{}, []NodeId{})
	r.BecomeLeader()
	res, newTerm := r.HandleAppendEntries(
		&raftrpc.AppendEntriesRequest{
			Term:         Term(2),
			PrevIndex:    -1,
			PrevTerm:     Term(-1),
			Entries:      []*raftrpc.LogEntry{raftlog.MakeLogEntry(1, "some entry")},
			LeaderCommit: Index(-1),
		},
	)
	assert.True(t, res)
	assert.True(t, r.IsFollower())
	assert.Equal(t, newTerm, Term(2))
	// Check the log was successfully appended
	assert.Equal(t, 1, len(r.log.Entries))
}

func TestDiscoverHigherTermAsLeaderFromAppendResponse(t *testing.T) {
	r := NewRaftState(make(chan OutboxMessage), MockAppSm{}, []NodeId{})
	r.BecomeLeader()
	r.HandleAppendEntriesResponse(-1, false, Term(2), 1, 0)
	// We should have become a follower
	assert.True(t, r.IsFollower())
	assert.Equal(t, r.currentTerm, Term(2))
}

func TestDiscoverNewLeaderAsCandidate(t *testing.T) {
	r := NewRaftState(make(chan OutboxMessage), MockAppSm{}, []NodeId{})
	r.BecomeCandidate()
	res, newTerm := r.HandleAppendEntries(
		&raftrpc.AppendEntriesRequest{
			Term:         Term(2),
			PrevIndex:    -1,
			PrevTerm:     Term(-1),
			Entries:      []*raftrpc.LogEntry{raftlog.MakeLogEntry(1, "some entry")},
			LeaderCommit: Index(-1),
		},
	)
	assert.True(t, r.IsFollower())
	assert.True(t, res)
	assert.Equal(t, newTerm, Term(2))
	// Check the log was successfully appended
	assert.Equal(t, 1, len(r.log.Entries))
}

func setUpEmptyLeader(t *testing.T) *RaftState {
	broadcastChan := make(chan OutboxMessage, 10)
	r := NewRaftState(broadcastChan, MockAppSm{}, []NodeId{1, 2, 3, 4})

	// put into the leader state
	r.statem.Fire(triggerElection)
	r.statem.Fire(winElection)
	assert.True(t, r.IsLeader())

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
				r.HandleAppendEntriesResponse(prevIndex, false, 1, i, numAppended)
			}
			for ; i < numSuccesses+numFailures; i++ {
				r.HandleAppendEntriesResponse(prevIndex, true, 1, i, numAppended)
			}

		default:
			panic(fmt.Sprintf("Unhandled message type %v", method))
		}
	}
}

type MockAppSm struct{}

func (m MockAppSm) Apply(item string) string {
	return "success"
}
