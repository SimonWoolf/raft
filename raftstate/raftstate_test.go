package raftstate

import (
	"context"
	"fmt"
	"log"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNoAppendWhenFollower(t *testing.T) {
	r := NewRaftState(make(chan ProtobufMessage))
	res, err := r.HandleClientLogAppend("item")
	assert.False(t, res)
	assert.NotNil(t, err)
}

func TestAppendResponseMajoritySuccess(t *testing.T) {
	r := setUpEmptyLeader(t)

	// mock a success response from the majority of other servers
	go mockResponses(r, Index(-1), 2, 3)

	// send it a request
	res, err := r.HandleClientLogAppend("item")
	assert.True(t, res)
	assert.Nil(t, err)
}

func TestAppendResponseMajorityFailure(t *testing.T) {
	r := setUpEmptyLeader(t)

	// mock a success response from the majority of other servers
	go mockResponses(r, Index(-1), 3, 3)

	// send it a request
	res, err := r.HandleClientLogAppend("item")
	assert.False(t, res)
	assert.NotNil(t, err)
}

func setUpEmptyLeader(t *testing.T) *RaftState {
	broadcastChan := make(chan ProtobufMessage, 1)
	r := NewRaftState(broadcastChan)

	// put into the leader state
	r.statem.Fire(triggerElection)
	r.statem.Fire(winElection)
	state, err := r.statem.State(context.Background())
	assert.Nil(t, err)
	assert.Equal(t, stateLeader, state)

	return r
}

func mockResponses(r *RaftState, prevIndex Index, numFailures int, numSuccesses int) {
	for {
		msg := (<-r.BroadcastChan).ProtoReflect()
		method := msg.Descriptor().Name()
		log.Printf("Got message on broadcast channel; method = %v", method)
		switch method {
		case "AppendEntriesRequest":
			i := 0
			for ; i < numFailures; i++ {
				r.HandleAppendEntriesResponse(prevIndex, false, i)
			}
			for ; i < numSuccesses+numFailures; i++ {
				r.HandleAppendEntriesResponse(prevIndex, true, i)
			}

		default:
			panic(fmt.Sprintf("Unhandled message type %v", method))
		}
	}
}
