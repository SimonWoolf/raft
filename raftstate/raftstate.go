package raftstate

import (
	"context"
	"errors"
	"raft/raftlog"
	"raft/raftrpc"

	"github.com/qmuntal/stateless"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// states
const (
	stateFollower  = "Follower"
	stateCandidate = "Candidate"
	stateLeader    = "Leader"
)

// state changes
const (
	triggerElection    = "TriggerElection"
	winElection        = "WinElection"
	timeoutElection    = "TimeOutElection"
	discoverHigherTerm = "DiscoverHigherTerm"
	discoverLeader     = "DiscoverLeader"
)

type PendingClientReq struct {
	ResultChannel chan error
	Responses     []bool
}

type ProtobufMessage interface {
	ProtoMessage()
	ProtoReflect() protoreflect.Message
}

type RaftState struct {
	log               *raftlog.RaftLog
	statem            *stateless.StateMachine
	currentTerm       int32
	pendingClientReqs []PendingClientReq
	broadcastChan     chan ProtobufMessage
}

func NewRaftState(broadcastChan chan ProtobufMessage) *RaftState {
	// First configure the state machine
	statem := stateless.NewStateMachine(stateFollower)

	statem.Configure(stateFollower).
		Permit(triggerElection, stateCandidate)

	statem.Configure(stateCandidate).
		Permit(winElection, stateLeader).
		PermitReentry(timeoutElection).
		Permit(discoverLeader, stateFollower)

	statem.Configure(stateLeader).
		Permit(discoverHigherTerm, stateFollower)

	return &RaftState{
		log:           &raftlog.RaftLog{},
		statem:        statem,
		currentTerm:   1,
		broadcastChan: broadcastChan,
	}
}

// A client of Raft makes a request to add a new log entry
// to the leader. The leader should take the new entry and use
// append_entries() to add it to its own log. This is how new log entries get
// added to a Raft cluster.
func (r *RaftState) HandleClientLogAppend(item string) (bool, error) {
	currentState, err := r.statem.State(context.Background())
	if err != nil {
		panic(err)
	}
	if currentState != stateLeader {
		return false, errors.New("Only the leader can handle client requests")
	}

	entry := raftlog.MakeLogEntry(int(r.currentTerm), item)
	entries := []raftlog.LogEntry{entry}
	prevIndex := int32(len(r.log.Entries))
	var prevTerm int32 = -1
	if prevIndex > 0 {
		prevTerm = r.log.Entries[prevIndex].Term
	}
	r.log.AppendEntries(prevIndex, prevTerm, entries)

	// Send the update to all our followers in parallel. Wait until we have
	// replies from the majority (nil error implies success), then reply to the
	// client.
	resultChan := make(chan error, 1)
	r.pendingClientReqs = append(r.pendingClientReqs, PendingClientReq{
		ResultChannel: resultChan,
		Responses:     []bool{},
	})

	r.broadcastChan <- &raftrpc.AppendEntriesRequest{
		Term:      int32(r.currentTerm),
		PrevIndex: prevIndex,
		PrevTerm:  prevTerm,
		Entries:   []*raftrpc.LogEntry{entry},
	}

	err = <-resultChan
	return err != nil, err
}

// A message sent by the Raft leader to a follower. This
// message contains log entries that should be added to the follower log.
// When received by a follower, it uses append_entries() to carry out the
// operation and responds with an AppendEntriesResponse message to indicate
// success or failure.
func (r *RaftState) HandleAppendEntries() {
}

// A message sent by a follower back to the Raft leader to indicate
// success/failure of an earlier AppendEntries message. A failure tells the
// leader to retry the AppendEntries with earlier log entries.
func (r *RaftState) HandleAppendEntriesResponse(success bool, nodeIndex int) {
}

// Expiration of a heartbeat timer on the leader. When received, the leader
// sends an AppendEntries message to all of the followers. This message will
// include all newly added log entries since the last heartbeat (note: there
// might be none).
func (r *RaftState) HandleLeaderHeartbeat() {
}
