package raftlog

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

type termSpec [2]int

func TestAppendEntriesMissing(t *testing.T) {
	currentTerm := 3
	length := 5
	log := makeSimpleLog(length, currentTerm)
	newEntries := []LogEntry{MakeLogEntry(currentTerm, "new item")}

	// should fail if there's missing entries. NB: it's the index of the _previous_
	// entry, and we're zero-indexing, so the 'equal to length' case should fail,
	// there should not already be an entry at that position
	assert.False(t, log.AppendEntries(int32(length), int32(currentTerm), newEntries))
	assert.Equal(t, 5, len(log.Entries))
}

func TestAppendEntriesAddingToEnd(t *testing.T) {
	currentTerm := 3
	length := 5
	log := makeSimpleLog(length, currentTerm)
	newEntries := []LogEntry{MakeLogEntry(currentTerm, "new item")}

	// should succeed if pushing right on the end
	assert.True(t, log.AppendEntries(int32(length-1), int32(currentTerm), newEntries))
	assert.Equal(t, 6, len(log.Entries))
	assert.Equal(t, "new item", log.Entries[5].Item)
}

func TestAppendEntriesReplacing(t *testing.T) {
	currentTerm := 3
	length := 5
	log := makeSimpleLog(length, currentTerm)
	// nb: current+1 to make sure it does actually replace
	newEntries := []LogEntry{MakeLogEntry(currentTerm+1, "new item")}

	// should succeed if replacing an existing entry
	assert.True(t, log.AppendEntries(int32(length-2), int32(currentTerm), newEntries))
	assert.Equal(t, 5, len(log.Entries))
	assert.Equal(t, "new item", log.Entries[4].Item)
}

func TestAppendEntriesTermMatch(t *testing.T) {
	currentTerm := 3
	length := 5
	log := makeSimpleLog(length, currentTerm)
	newEntries := []LogEntry{MakeLogEntry(currentTerm, "new item")}

	// should fail if term doesn't match the prevTerm
	newEntries = []LogEntry{MakeLogEntry(currentTerm+1, "new item")}
	assert.False(t, log.AppendEntries(int32(length-1), int32(currentTerm+1), newEntries))
}

func TestAppendEntriesToEmpty(t *testing.T) {
	// check the empty-log case (no previous entry) case works
	log := makeSimpleLog(0, 0)
	newEntries := []LogEntry{MakeLogEntry(1, "new item")}
	assert.True(t, log.AppendEntries(-1, -1, newEntries))
	assert.Equal(t, 1, len(log.Entries))
}

func TestEntryConflict(t *testing.T) {
	length := 5
	oldTerm := 3
	newTerm := 4
	log := makeSimpleLog(length, oldTerm)
	newEntries := []LogEntry{MakeLogEntry(newTerm, "new item")}

	// Let's say we want the last three entries to conflict, due to increased
	// term. We want the last three entries to all be deleted, and a new one
	// inserted, so new length should be 3
	assert.True(t, log.AppendEntries(1, int32(oldTerm), newEntries))
	assert.Equal(t, "item 1", log.Entries[1].Item) // 1 is unchanged
	assert.Equal(t, newEntries[0], log.Entries[2]) // 2 is replaced
	assert.Equal(t, 3, len(log.Entries))           // 4 and 5 are gone
}

// Construct the set of logs described by fig 7 of the raft paper, do the same
// operation to each, and check the outcome
func TestFigSeven(t *testing.T) {
	logs := []*RaftLog{
		makeLogToSpec([]termSpec{{1, 3}, {4, 2}, {5, 2}, {6, 2}}),
		makeLogToSpec([]termSpec{{1, 3}, {4, 1}}),
		makeLogToSpec([]termSpec{{1, 3}, {4, 2}, {5, 2}, {6, 4}}),
		makeLogToSpec([]termSpec{{1, 3}, {4, 2}, {5, 2}, {6, 3}, {7, 2}}),
		makeLogToSpec([]termSpec{{1, 3}, {4, 4}}),
		makeLogToSpec([]termSpec{{1, 3}, {2, 3}, {3, 5}}),
	}

	newEntries := []LogEntry{MakeLogEntry(8, "x")}

	// (a) False. Missing entry at index 10.
	assert.False(t, logs[0].AppendEntries(9, 6, newEntries))
	// ((b) False. Many missing entries.
	assert.False(t, logs[1].AppendEntries(9, 6, newEntries))
	// ((c) True. Entry already in position 10 is replaced.
	assert.True(t, logs[2].AppendEntries(9, 6, newEntries))
	// ((d) True. Entries at position 10,11 are replaced.
	assert.True(t, logs[3].AppendEntries(9, 6, newEntries))
	// ((e) False. Missing entries.
	assert.False(t, logs[4].AppendEntries(9, 6, newEntries))
	// ((f) False. Previous term mismatch.
	assert.False(t, logs[5].AppendEntries(9, 6, newEntries))
}

func makeSimpleLog(length int, currentTerm int) *RaftLog {
	return addEntries(&RaftLog{}, []termSpec{{currentTerm, length}})
}

func makeLogToSpec(spec []termSpec) *RaftLog {
	return addEntries(&RaftLog{}, spec)
}

// for constructing mock logs to a particular spec
func addEntries(log *RaftLog, spec []termSpec) *RaftLog {
	for _, v := range spec {
		term, numInTerm := v[0], v[1]
		for i := 0; i < numInTerm; i++ {
			log.Entries = append(log.Entries, MakeLogEntry(term, fmt.Sprintf("item %d", i)))
		}
	}
	return log
}
