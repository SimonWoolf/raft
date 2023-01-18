package raftstate

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNoAppendWhenFollower(t *testing.T) {
	r := NewRaftState(make(chan ProtobufMessage))
	res, err := r.HandleClientLogAppend("item")
	assert.False(t, res)
	assert.NotNil(t, err)
}

func TestAppendResponseFromMajority(t *testing.T) {
	broadcastChan := make(chan ProtobufMessage, 1)
	r := NewRaftState(broadcastChan)

	// put into the leader state
	r.statem.Fire(triggerElection)
	r.statem.Fire(winElection)
	state, err := r.statem.State(context.Background())
	assert.Nil(t, err)
	assert.Equal(t, stateLeader, state)

	// in the background, mock a success response from the majority of other servers
	go func() {
		for {
			msg := (<-broadcastChan).ProtoReflect()
			method := msg.Descriptor().Name()
			switch method {
			case "AppendEntriesRequest":
				// success response from the first three
				for i := 0; i < 3; i++ {
					r.HandleAppendEntriesResponse(true, i)
				}

			default:
				panic("Unhandled message type " + method)
			}
		}
	}()

	// send it a request
	res, err := r.HandleClientLogAppend("item")
	fmt.Printf("### res = %v; err = %v\n", res, err)
}
