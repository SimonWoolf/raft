package raftstate

import (
	"context"
	"errors"
	"log"
	"raft/raftlog"
	"raft/raftrpc"
	"raft/utils"

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

const clusterSize = 5
const repliesForMajority = 2 // 2 other servers plus ourselves is a majority

type Index = raftlog.Index
type Term = raftlog.Term
type NodeId = int

type ProtobufMessage interface {
	ProtoMessage()
	ProtoReflect() protoreflect.Message
}

type BroadcastMsgType string

const (
	AppendMsgType BroadcastMsgType = "AppendEntries"
)

type OutboxMessage struct {
	Msg        ProtobufMessage
	MsgType    BroadcastMsgType // obligatory grumble at the lack of proper tagged unions
	Recipients []NodeId
}

type PendingClientReq struct {
	ResultChannel chan error
	Responses     *[clusterSize]*bool // use pointer to bool as a crude
	// Maybe[bool], so we can distinguish no reply yet from false reply
	// TODO actually a negative reply doesn't need to be stored, as it results in
	// a retry at a lower index -- can we go for just normal bool...?
}

type RaftState struct {
	log           *raftlog.RaftLog
	statem        *stateless.StateMachine
	currentTerm   Term
	BroadcastChan chan OutboxMessage
	otherNodeIds  []NodeId

	// indexed by prevIndex. Note that a request can continue to be pending while
	// the leader goes back and forth with a follower, backfilling earlier
	// indices; it will be fulfilled when it reaches the current one. We don't
	// keep pendingClientReqs for the backfills, only when there's someone waiting
	// on the result
	pendingClientReqs map[Index]PendingClientReq

	// for each server, index of the next log entry to send to that server
	// (initialized to leader last log index + 1)
	nextIndices map[NodeId]Index

	// for each server, index of highest log entry known to be replicated on
	// server (initialized to 0, increases monotonically
	matchIndices map[NodeId]Index
}

func NewRaftState(broadcastChan chan OutboxMessage, otherNodeIds []NodeId) *RaftState {
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

	matchIndices := make(map[NodeId]Index)
	nextIndices := make(map[NodeId]Index)
	for _, nodeId := range otherNodeIds {
		matchIndices[nodeId] = 0
		nextIndices[nodeId] = 0 // TODO should be initialized to leader log index + 1 on becoming leader
	}

	return &RaftState{
		log:               &raftlog.RaftLog{},
		statem:            statem,
		currentTerm:       1,
		pendingClientReqs: make(map[Index]PendingClientReq),
		BroadcastChan:     broadcastChan,
		matchIndices:      matchIndices,
		nextIndices:       nextIndices,
		otherNodeIds:      otherNodeIds,
	}
}

// A client of Raft makes a request to add a new log entry
// to the leader. The leader should take the new entry and use
// append_entries() to add it to its own log. This is how new log entries get
// added to a Raft cluster.
func (r *RaftState) HandleClientLogAppend(item string) (bool, error) {
	currentState := utils.MustSucceed(r.statem.State(context.Background()))
	if currentState != stateLeader {
		return false, errors.New("Only the leader can handle client requests")
	}

	entry := raftlog.MakeLogEntry(int(r.currentTerm), item)
	entries := []raftlog.LogEntry{entry}
	prevIndex := Index(len(r.log.Entries) - 1)
	var prevTerm Term = -1
	if prevIndex >= 0 {
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

	r.BroadcastChan <- OutboxMessage{
		Msg: &raftrpc.AppendEntriesRequest{
			Term:      Term(r.currentTerm),
			PrevIndex: prevIndex,
			PrevTerm:  prevTerm,
			Entries:   []*raftrpc.LogEntry{entry},
		},
		MsgType:    AppendMsgType,
		Recipients: r.otherNodeIds,
	}

	err := <-resultChan
	return err == nil, err
}

// A message sent by the Raft leader to a follower. This
// message contains log entries that should be added to the follower log.
// When received by a follower, it uses append_entries() to carry out the
// operation and responds with an AppendEntriesResponse message to indicate
// success or failure.
func (r *RaftState) HandleAppendEntries(req *raftrpc.AppendEntriesRequest) bool {
	currentState := utils.MustSucceed(r.statem.State(context.Background()))
	if currentState != stateFollower {
		log.Printf("Received append entries req from leader %d, but unable to handle as in state %v", req.NodeId, currentState)
		return false
	}

	appendRes := r.log.AppendEntries(req.PrevIndex, req.PrevTerm, req.Entries)

	log.Printf("Handled append entries from leader %d; prevIndex = %v; prevTerm = %v; result = %v\n", req.NodeId, req.PrevIndex, req.PrevTerm, appendRes)

	return appendRes
}

// A message sent by a follower back to the Raft leader to indicate
// success/failure of an earlier AppendEntries message. A failure tells the
// leader to retry the AppendEntries with earlier log entries.
func (r *RaftState) HandleAppendEntriesResponse(prevIndex Index, success bool, nodeId NodeId, numAppended Index) {
	if !success {
		// • If AppendEntries fails because of log inconsistency: decrement
		// nextIndex and retry (§5.3)
		//
		// NB: the Term in the request is not the term of the entry being sent.
		// The entries are LogEntries, they each have their own terms. It's just
		// the current leader's term, used to check the validity of the request.
		//
		// We don't set up a pending client request for this -- it's not a client
		// request. There may still be a pending client req for the original entry
		// to be replicated, and that'll be fulfilled by the response if this
		// succeeds.
		index := prevIndex
		prevIndex = utils.Max(index-1, -1)
		var prevTerm Term = -1
		if prevIndex >= 0 {
			prevTerm = r.log.Entries[prevIndex].Term
		}

		r.nextIndices[nodeId] = prevIndex // TODO is this right?
		logEntries := r.log.GetEntriesFrom(index)

		r.BroadcastChan <- OutboxMessage{
			Msg: &raftrpc.AppendEntriesRequest{
				Term:      Term(r.currentTerm),
				PrevIndex: prevIndex,
				PrevTerm:  prevTerm,
				Entries:   logEntries,
			},
			MsgType:    AppendMsgType,
			Recipients: []NodeId{nodeId},
		}
		return
	}

	// • If successful: update nextIndex and matchIndex for
	// follower (§5.3)
	r.matchIndices[nodeId] = utils.Max(r.matchIndices[nodeId], prevIndex+numAppended)
	r.nextIndices[nodeId] = prevIndex + 1 + numAppended

	// Update any pending requests for any of the indices we've replicated
	for i := prevIndex; i < prevIndex+numAppended; i++ {
		pendingReq, exists := r.pendingClientReqs[i]
		if !exists {
			// eg for a req that we've already responded to, say because we've heard a
			// success from the majority
			log.Printf("Ignoring append entries response for index = %v with no corresponding pending client request (but success = %v; nodeIndex = %v)", i, success, nodeId)
			return
		}
		pendingReq.Responses[nodeId] = &success

		numReplies, numSuccesses := 0, 0
		for _, v := range pendingReq.Responses {
			if v != nil {
				numReplies++
				if *v == true {
					numSuccesses++
				}
			}
		}

		log.Printf("Handling append entries response; prevIndex = %v; success = %v; nodeIndex = %v; numSuccesses = %v; numReplies = %v\n", prevIndex, success, nodeId, numSuccesses, numReplies)

		if numSuccesses >= repliesForMajority {
			pendingReq.ResultChannel <- nil
			delete(r.pendingClientReqs, prevIndex)
		} else if numReplies >= clusterSize-1 {
			// TODO this case can't actually happen since on !success we retry with
			// an earlier index, so leave the entry blank
			pendingReq.ResultChannel <- errors.New("Unsuccessful result from a majority of nodes")
			delete(r.pendingClientReqs, prevIndex)
		}
	}
}

// Temp, remove once we have actual stuff working
func (r *RaftState) BecomeLeader() {
	currentState := utils.MustSucceed(r.statem.State(context.Background()))
	if currentState == stateFollower {
		r.statem.Fire(triggerElection)
		r.statem.Fire(winElection)
	}
}

// includes uncommited entries
func (r *RaftState) GetAllEntries() []string {
	return utils.Map(r.log.Entries, func(le raftlog.LogEntry) string { return le.Item })
}

// Expiration of a heartbeat timer on the leader. When received, the leader
// sends an AppendEntries message to all of the followers. This message will
// include all newly added log entries since the last heartbeat (note: there
// might be none).
func (r *RaftState) HandleLeaderHeartbeat() {
}
