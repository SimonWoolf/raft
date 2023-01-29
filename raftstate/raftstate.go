package raftstate

import (
	"context"
	"errors"
	"log"
	"raft/raftlog"
	"raft/raftrpc"
	"raft/utils"

	"github.com/qmuntal/stateless"
	"golang.org/x/exp/maps"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type ApplicationStateMachine interface {
	// In general, the ApplicationStateMachine can block on Apply. Our KV server
	// happen so not, but the raft state should be able to cope with Apply taking
	// a while.
	Apply(item string) string
}

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
	MsgType    BroadcastMsgType // obligatory grumble at the lack aaof proper tagged unions
	Recipients []NodeId
}

type ClientResponseMessage = string

type RaftState struct {
	log           *raftlog.RaftLog
	statem        *stateless.StateMachine
	currentTerm   Term
	BroadcastChan chan OutboxMessage
	ApplicationSM ApplicationStateMachine
	otherNodeIds  []NodeId

	//=== Volatile leader state ===

	// indexed by prevIndex; channels to send the result of the state machine
	// application to. Could put these response channels in the RaftLog together
	// with the items they apply to, but it seems neater to keep them separate in
	// state that's cleared when we stop becoming the leader
	pendingClientReqs map[Index](chan ClientResponseMessage)

	// for each server, index of the next log entry to send to that server
	// (initialized to leader last log index + 1)
	nextIndices map[NodeId]Index

	// for each server, index of highest log entry known to be replicated on
	// server (initialized to 0, increases monotonically.
	// (NB: we actually initialize to -1 because we're using zero-indexing, paper
	// uses 1-indexing)
	matchIndices map[NodeId]Index

	//=== Volatile all-node state ===

	// index of highest log entry known to be committed (initialized to 0
	// [actually -1, see matchIndices], increases monotonically).
	commitIndex Index
	// index of highest log entry applied to state machine (initialized to 0,
	// [actually -1, see matchIndices], increases monotonically)
	lastApplied Index
}

func NewRaftState(broadcastChan chan OutboxMessage, applicationSM ApplicationStateMachine, otherNodeIds []NodeId) *RaftState {
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
		matchIndices[nodeId] = -1
		nextIndices[nodeId] = 0 // TODO should be initialized to leader log index + 1 on becoming leader
	}

	return &RaftState{
		log:               &raftlog.RaftLog{},
		statem:            statem,
		currentTerm:       1,
		pendingClientReqs: make(map[Index](chan ClientResponseMessage)),
		BroadcastChan:     broadcastChan,
		matchIndices:      matchIndices,
		nextIndices:       nextIndices,
		otherNodeIds:      otherNodeIds,
		commitIndex:       -1,
		lastApplied:       -1,
		ApplicationSM:     applicationSM,
	}
}

// A client of Raft makes a request to add a new log entry
// to the leader. The leader should take the new entry and use
// append_entries() to add it to its own log. This is how new log entries get
// added to a Raft cluster.
func (r *RaftState) HandleClientLogAppend(item string) (string, error) {
	currentState := utils.MustSucceed(r.statem.State(context.Background()))
	if currentState != stateLeader {
		return "", errors.New("Only the leader can handle client requests")
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
	// TODO does this need to be buffered at all?
	resultChan := make(chan ClientResponseMessage, 1)
	r.pendingClientReqs[prevIndex+1] = resultChan

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

	log.Printf("Awaiting response for index %v\n", prevIndex+1)
	clientResponse := <-resultChan
	log.Printf("Got response for index %v; replying to client\n", prevIndex+1)
	return clientResponse, nil
}

// A message sent by the Raft leader to a follower. This
// message contains log entries that should be added to the follower log.
// When received by a follower, it uses append_entries() to carry out the
// operation and responds with an AppendEntriesResponse message to indicate
// success or failure.
func (r *RaftState) HandleAppendEntries(req *raftrpc.AppendEntriesRequest) (bool, Term) {
	// If RPC request or response contains term T > currentTerm:
	// set currentTerm = T, convert to follower (§5.1)
	if req.Term > r.currentTerm {
		r.currentTerm = req.Term
		if r.IsLeader() {
			log.Printf("Received append entries req as leader from someone (node %d) with a higher term (%d) than us; becoming a follower", req.LeaderId, req.Term)
			r.statem.Fire(discoverHigherTerm)
		} else if r.IsCandidate() {
			log.Printf("Received append entries req as candidate from node (%d) with a higher term (%d) than us; becoming a follower", req.LeaderId, req.Term)
			r.statem.Fire(discoverLeader)
		}
	}

	if !r.IsFollower() {
		log.Printf("Received append entries req from leader %d, but unable to handle as in state %v", req.LeaderId, utils.MustSucceed(r.statem.State(context.Background())))
		return false, r.currentTerm
	}

	// 1. Reply false if term < currentTerm (§5.1)
	if req.Term < r.currentTerm {
		return false, r.currentTerm
	}

	// 2. Reply false if log doesn’t contain an entry at prevLogIndex
	// whose term matches prevLogTerm (§5.3)
	// 3. If an existing entry conflicts with a new one (same index
	// but different terms), delete the existing entry and all that
	// follow it (§5.3)
	// 4. Append any new entries not already in the log
	// ^^^ all checks which do are not functions of raftstate are handled by the RaftLog
	appendRes := r.log.AppendEntries(req.PrevIndex, req.PrevTerm, req.Entries)

	// 5. If leaderCommit > commitIndex, set commitIndex =
	// min(leaderCommit, index of last new entry)
	if appendRes && req.LeaderCommit > r.commitIndex {
		r.commitIndex = utils.Min(req.LeaderCommit, req.PrevIndex+Index(len(req.Entries)))
		r.updateStateMachine()
	}

	log.Printf("Handled append entries from leader %d; prevIndex = %v; prevTerm = %v; result = %v\n", req.LeaderId, req.PrevIndex, req.PrevTerm, appendRes)

	return appendRes, r.currentTerm
}

// A message sent by a follower back to the Raft leader to indicate
// success/failure of an earlier AppendEntries message. A failure tells the
// leader to retry the AppendEntries with earlier log entries.
func (r *RaftState) HandleAppendEntriesResponse(prevIndex Index, success bool, term Term, nodeId NodeId, numAppended Index) {
	// If RPC request or response contains term T > currentTerm:
	// set currentTerm = T, convert to follower (§5.1)
	if term > r.currentTerm {
		r.currentTerm = term
		if r.IsLeader() {
			log.Printf("Received append entries response from someone (node %d) with a higher term (%d) than us; becoming a follower", nodeId, term)
			r.statem.Fire(discoverHigherTerm)
		} else if r.IsCandidate() {
			log.Printf("Received append entries resp candidate from node (%d) with a higher term (%d) than us; becoming a follower", nodeId, term)
			r.statem.Fire(discoverLeader)
		}
	}

	if !r.IsLeader() {
		log.Printf("Ignoring append entries response from %v as we're no longer the leader", nodeId)
		return
	}

	log.Printf("Handling append entries response; prevIndex = %v; success = %v; nodeIndex = %v\n", prevIndex, success, nodeId)

	if success {
		// • If successful: update nextIndex and matchIndex for
		// follower (§5.3)
		log.Printf("Updating match index for nodeid %v, prevIndex = %v, numAppended = %v, matchIndex = %v", nodeId, prevIndex, numAppended, utils.Max(r.matchIndices[nodeId], prevIndex+numAppended))
		r.matchIndices[nodeId] = utils.Max(r.matchIndices[nodeId], prevIndex+numAppended)
		r.nextIndices[nodeId] = prevIndex + 1 + numAppended
		r.leaderUpdateCommitIndex()
	} else if prevIndex > -1 {
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
		//
		index := utils.Max(prevIndex, 0)
		var prevTerm Term
		if index == 0 {
			prevIndex = -1
			prevTerm = Term(-1)
		} else {
			prevIndex = index - 1
			prevTerm = r.log.Entries[prevIndex].Term
		}

		// the nextIndex is updated to the index we're currently sending, unless &
		// until we hear back and confirm that one is successfully applied
		r.nextIndices[nodeId] = index
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
	} else {
		// If the prevIndex was already -1, there's no further decrementing we can
		// do, and no point retrying as the request will be identical. Continue on
		// to mark it as a failure for pendingClientReq purposes. (This should
		// never happen in production, but is convenient to be able to do this for
		// test purposes)
		log.Printf("Append entries response for a request containing the complete log unexpectedly failed; nodeId = %v", nodeId)
	}
}

// Should be called immediately on the leader after anything mutates matchIndices
func (r *RaftState) leaderUpdateCommitIndex() {
	// If there exists an N such that N > commitIndex, a majority
	// of matchIndex[i] ≥ N, and log[N].term == currentTerm:
	// set commitIndex = N

	myLastLogIndex := Index(len(r.log.Entries) - 1)
	allLastLogIndices := append(maps.Values(r.matchIndices), myLastLogIndex)

	// The median replicated log index (or the largest one <= that that satisfies
	// the term constraint) is the N we want.
	medianIndex := utils.Median(allLastLogIndices)
	for i := medianIndex; i > r.commitIndex; i-- {
		if r.log.Entries[i].Term == r.currentTerm {
			r.commitIndex = i
			r.updateStateMachine()
			return
		}
	}
}

// Should be called on all roles once anything changes commitIndex
func (r *RaftState) updateStateMachine() {
	// If commitIndex > lastApplied: increment lastApplied, apply
	// log[lastApplied] to state machine (§5.3)
	if r.commitIndex > r.lastApplied {
		r.lastApplied++
		item := r.log.Entries[r.lastApplied].Item
		log.Printf("Sending item with index %v to state machine; item = '%s'", r.lastApplied, item)
		// In general the applicationStateMachine can block on apply
		result := r.ApplicationSM.Apply(item)
		log.Printf("Got result '%s' from state machine", result)
		resultChan, exists := r.pendingClientReqs[r.lastApplied]
		if exists {
			delete(r.pendingClientReqs, r.lastApplied)
			resultChan <- result
		}
		// repeat until lastApplied is caught up
		r.updateStateMachine()
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

func (r *RaftState) BecomeCandidate() {
	currentState := utils.MustSucceed(r.statem.State(context.Background()))
	if currentState == stateFollower {
		r.statem.Fire(triggerElection)
	} else if currentState == stateLeader {
		r.statem.Fire(discoverHigherTerm)
		r.statem.Fire(triggerElection)
	}
}

func (r *RaftState) IsLeader() bool {
	return utils.MustSucceed(r.statem.IsInState(stateLeader))
}

func (r *RaftState) IsFollower() bool {
	return utils.MustSucceed(r.statem.IsInState(stateFollower))
}

func (r *RaftState) IsCandidate() bool {
	return utils.MustSucceed(r.statem.IsInState(stateCandidate))
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
