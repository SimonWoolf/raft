package raftstate

import (
	"context"
	"errors"
	"log"
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

type NodeIndex int

const clusterSize = 5
const majority = 3

type Index = raftlog.Index
type Term = raftlog.Term

type ProtobufMessage interface {
	ProtoMessage()
	ProtoReflect() protoreflect.Message
}

type PendingClientReq struct {
	ResultChannel chan error
	Responses     *[clusterSize]*bool // use pointer to bool as a crude
	// Maybe[bool], so we can distinguish no reply yet from false reply
}

type RaftState struct {
	log               *raftlog.RaftLog
	statem            *stateless.StateMachine
	currentTerm       Term
	pendingClientReqs map[Index]PendingClientReq // indexed by prevIndex
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
		log:               &raftlog.RaftLog{},
		statem:            statem,
		currentTerm:       1,
		pendingClientReqs: make(map[Index]PendingClientReq),
		broadcastChan:     broadcastChan,
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
	prevIndex := Index(len(r.log.Entries) - 1)
	var prevTerm Term = -1
	if prevIndex > 0 {
		prevTerm = r.log.Entries[prevIndex].Term
	}
	r.log.AppendEntries(prevIndex, prevTerm, entries)

	log.Printf("Handling client log append; prevIndex = %v; prevTerm = %v; item = %s\n", prevIndex, prevTerm, item)

	// Send the update to all our followers in parallel. Wait until we have
	// replies from the majority (nil error implies success), then reply to the
	// client.
	resultChan := make(chan error, 1)
	r.pendingClientReqs[prevIndex] = PendingClientReq{
		ResultChannel: resultChan,
		Responses:     &[clusterSize]*bool{},
	}

	r.broadcastChan <- &raftrpc.AppendEntriesRequest{
		Term:      Term(r.currentTerm),
		PrevIndex: prevIndex,
		PrevTerm:  prevTerm,
		Entries:   []*raftrpc.LogEntry{entry},
	}

	err = <-resultChan
	return err == nil, err
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
func (r *RaftState) HandleAppendEntriesResponse(prevIndex Index, success bool, nodeIndex int) {
	pendingReq, exists := r.pendingClientReqs[prevIndex]
	if !exists {
		// eg for a req that we've already responded to, say because we've heard a
		// success from the majority
		log.Printf("Ignoring append entries response for index = %v that we no longer care about (but success = %v; nodeIndex = %v)", prevIndex, success, nodeIndex)
		return
	}

	pendingReq.Responses[nodeIndex] = &success

	numReplies, numSuccesses := 0, 0
	for _, v := range pendingReq.Responses {
		if v != nil {
			numReplies++
			if *v == true {
				numSuccesses++
			}
		}
	}

	log.Printf("Handling append entries response; prevIndex = %v; success = %v; nodeIndex = %v; numSuccesses = %v; numReplies = %v\n", prevIndex, success, nodeIndex, numSuccesses, numReplies)

	if numSuccesses >= majority {
		pendingReq.ResultChannel <- nil
		delete(r.pendingClientReqs, prevIndex)
	} else if numReplies >= clusterSize {
		pendingReq.ResultChannel <- errors.New("Unsuccessful result from a majority of nodes")
		delete(r.pendingClientReqs, prevIndex)
	}
}

// Expiration of a heartbeat timer on the leader. When received, the leader
// sends an AppendEntries message to all of the followers. This message will
// include all newly added log entries since the last heartbeat (note: there
// might be none).
func (r *RaftState) HandleLeaderHeartbeat() {
}
