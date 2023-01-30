package raftstate

import (
	"fmt"
	"log"
	"raft/raftlog"
	"raft/raftrpc"
	rpc "raft/raftrpc"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNoClientAppendWhenFollower(t *testing.T) {
	r := startStandaloneRaftState()
	resp := r.ClientLogAppend(&rpc.ClientLogAppendRequest{
		Item: "item",
	})
	assert.NotEmpty(t, resp.Error)
}

func TestNoAppendWhenNewerTerm(t *testing.T) {
	r := startStandaloneRaftState()
	r.currentTerm = 2
	resp := r.AppendEntries(
		&raftrpc.AppendEntriesRequest{
			Term:         Term(1),
			PrevIndex:    -1,
			PrevTerm:     Term(-1),
			Entries:      []*raftrpc.LogEntry{raftlog.MakeLogEntry(1, "some entry")},
			LeaderCommit: Index(-1),
		},
	)
	assert.False(t, resp.Result)
}

func TestNoAppendWhenLeader(t *testing.T) {
	r := startStandaloneRaftState()
	r.BecomeLeader()
	resp := r.AppendEntries(
		&raftrpc.AppendEntriesRequest{
			Term:         Term(1),
			PrevIndex:    -1,
			PrevTerm:     Term(-1),
			Entries:      []*raftrpc.LogEntry{raftlog.MakeLogEntry(1, "some entry")},
			LeaderCommit: Index(-1),
		},
	)
	assert.False(t, resp.Result)
}

// Checks which are only functions of the raftlog are tested more thoroughly in
// raftlog_test. Here we check one failure and one success case to check the
// integration.
func TestNoAppendWhenNoPreviousItem(t *testing.T) {
	r := startStandaloneRaftState()
	resp := r.AppendEntries(
		&raftrpc.AppendEntriesRequest{
			Term:         Term(1),
			PrevIndex:    3,
			PrevTerm:     Term(1),
			Entries:      []*raftrpc.LogEntry{raftlog.MakeLogEntry(1, "some entry")},
			LeaderCommit: Index(-1),
		},
	)
	assert.False(t, resp.Result)
}

func TestAppendCanSucceed(t *testing.T) {
	r := startStandaloneRaftState()
	// default to follower
	assert.True(t, r.IsFollower())
	resp := r.AppendEntries(
		&raftrpc.AppendEntriesRequest{
			Term:         Term(1),
			PrevIndex:    -1,
			PrevTerm:     Term(-1),
			Entries:      []*raftrpc.LogEntry{raftlog.MakeLogEntry(1, "some entry")},
			LeaderCommit: Index(-1),
		},
	)
	assert.True(t, resp.Result)
}

func TestAppendResponseMajoritySuccess(t *testing.T) {
	r := setUpEmptyLeader(t)
	assert.EqualValues(t, -1, r.commitIndex)
	assert.EqualValues(t, -1, r.lastApplied)

	// mock a success response from the majority of servers
	// (2 others is a majority as we can count ourselves)
	go mockResponses(r, 2, 2)

	// send it a request
	resp := r.ClientLogAppend(&rpc.ClientLogAppendRequest{
		Item: "item",
	})
	assert.Equal(t, "ok", resp.Response)
	assert.Empty(t, resp.Error)
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
//   go mockResponses(r,  3, 1)

//   // send it a request
//   _, err := r.
//   assert.NotNil(t, err)
//   assert.EqualValues(t, -1, r.commitIndex)
//   assert.EqualValues(t, -1, r.lastApplied)
// }

func TestDiscoverHigherTermAsLeaderFromAppend(t *testing.T) {
	r := startStandaloneRaftState()
	r.BecomeLeader()
	resp := r.AppendEntries(
		&raftrpc.AppendEntriesRequest{
			Term:         Term(2),
			PrevIndex:    -1,
			PrevTerm:     Term(-1),
			Entries:      []*raftrpc.LogEntry{raftlog.MakeLogEntry(1, "some entry")},
			LeaderCommit: Index(-1),
		},
	)
	assert.True(t, resp.Result)
	assert.True(t, r.IsFollower())
	assert.Equal(t, resp.Term, Term(2))
	// Check the log was successfully appended
	assert.Equal(t, 1, len(r.log.Entries))
}

func TestDiscoverHigherTermAsLeaderFromAppendResponse(t *testing.T) {
	r := startStandaloneRaftState()
	r.BecomeLeader()
	r.NoteEntryAppended(-1, false, Term(2), 1, 0)
	// We should have become a follower
	assert.True(t, r.IsFollower())
	assert.Equal(t, r.currentTerm, Term(2))
}

func TestDiscoverNewLeaderAsCandidate(t *testing.T) {
	r := startStandaloneRaftState()
	r.BecomeCandidate()
	resp := r.AppendEntries(
		&raftrpc.AppendEntriesRequest{
			Term:         Term(2),
			PrevIndex:    -1,
			PrevTerm:     Term(-1),
			Entries:      []*raftrpc.LogEntry{raftlog.MakeLogEntry(1, "some entry")},
			LeaderCommit: Index(-1),
		},
	)
	assert.True(t, r.IsFollower())
	assert.True(t, resp.Result)
	assert.Equal(t, resp.Term, Term(2))
	// Check the log was successfully appended
	assert.Equal(t, 1, len(r.log.Entries))
}

func TestConcurrentAccess(t *testing.T) {
	r := setUpEmptyLeader(t)
	// mock a success response from the majority of servers
	go mockResponses(r, 0, 4)

	r.BecomeLeader()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			resp := r.ClientLogAppend(&rpc.ClientLogAppendRequest{
				Item: fmt.Sprintf("item %d", i),
			})
			assert.Equal(t, "ok", resp.Response)
			assert.Empty(t, resp.Error)
			wg.Done()
		}(i)
	}
	wg.Wait()
	// Check the log was successfully appended
	fmt.Println(r.log.Entries)
	assert.Equal(t, 100, len(r.log.Entries))
}

func setUpEmptyLeader(t *testing.T) *RaftState {
	broadcastChan := make(chan OutboxMessage, 100)
	r := NewRaftState(broadcastChan, MockAppSm{}, []NodeId{1, 2, 3, 4})
	go r.Run()

	// put into the leader state
	r.statem.Fire(triggerElection)
	r.statem.Fire(winElection)
	assert.True(t, r.IsLeader())

	return r
}

// Mock the requested number of successes or failures in the initial broadcast;
// ignore subsequent retries for specific nodes
func mockResponses(r *RaftState, numFailures int, numSuccesses int) {
	for {
		msg := (<-r.BroadcastChan)
		method := msg.MsgType
		log.Printf("Got message on broadcast channel; method = %v", method)
		switch method {
		case AppendMsgType:
			req := msg.Msg.(*raftrpc.AppendEntriesRequest)
			numAppended := int32(len(req.Entries))
			if len(msg.Recipients) == 1 {
				// Ignore all except all-node broadcasts from the initial client req
				return
			}
			i := 0
			for ; i < numFailures; i++ {
				r.NoteEntryAppended(req.PrevIndex, false, req.Term, i, numAppended)
			}
			for ; i < numSuccesses+numFailures; i++ {
				r.NoteEntryAppended(req.PrevIndex, true, req.Term, i, numAppended)
			}

		default:
			panic(fmt.Sprintf("Unhandled message type %v", method))
		}
	}
}

func startStandaloneRaftState() *RaftState {
	// TODO this leaks a goroutine each time it runs, need to use contexts
	r := NewRaftState(make(chan OutboxMessage), MockAppSm{}, []NodeId{})
	go r.Run()
	return r
}

type MockAppSm struct{}

func (m MockAppSm) Apply(item string) string {
	return "ok"
}
