package raftlog

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func makeDummyLog(length int, currentTerm int) *RaftLog {
	log := RaftLog{}
	for i := 0; i < length; i++ {
		log.entries = append(log.entries, LogEntry{
			term: currentTerm,
			item: fmt.Sprintf("item %d", i),
		})
	}
	return &log
}

func TestAppendEntriesPrevLogEntry(t *testing.T) {
	currentTerm := 3
	length := 5
	log := makeDummyLog(length, currentTerm)
	newEntries := []LogEntry{LogEntry{term: currentTerm, item: "new item"}}

	// should fail if there's missing entries. NB: it's the index of the _previous_
	// entry, and we're zero-indexing, so the 'equal to length' case should fail,
	// there should not already be an entry at that position
	assert.False(t, log.AppendEntries(length, currentTerm, newEntries))
	// TODO test that the restul is what we expect

	// should succeed if pushing right on the end
	assert.True(t, log.AppendEntries(length-1, currentTerm, newEntries))

	// should succeed if replacing an existing entry
	assert.True(t, log.AppendEntries(length-2, currentTerm, newEntries))

	// should fail if term doesn't match the prevTerm
	newEntries = []LogEntry{LogEntry{term: currentTerm + 1, item: "new item"}}
	assert.False(t, log.AppendEntries(length-1, currentTerm+1, newEntries))

	// check the empty-log case (no previous entry) case works
	log = makeDummyLog(1, 0)
	assert.True(t, log.AppendEntries(-1, -1, newEntries))
}

func TestEntryConflict(t *testing.T) {
	length := 5
	oldTerm := 3
	newTerm := 4
	log := makeDummyLog(length, oldTerm)
	newEntries := []LogEntry{LogEntry{term: newTerm, item: "new item"}}

	// Let's say we want the last three entries to conflict, due to increased
	// term. We want the last three entries to all be deleted, and a new one
	// inserted, so new length should be 3
	fmt.Println("###")
	fmt.Printf("before: %v\n", log)
	assert.True(t, log.AppendEntries(1, oldTerm, newEntries))
	fmt.Printf("after: %v\n", log)
	assert.Equal(t, "item 1", log.entries[1].item) // 1 is unchanged
	assert.Equal(t, newEntries[0], log.entries[2]) // 2 is replaced
	assert.Equal(t, 3, len(log.entries))           // 4 and 5 are gone
}
