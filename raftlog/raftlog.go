package raftlog

import (
	"fmt"
	"log"
	"raft/utils"
	"strings"
)

type RaftLog struct {
	entries []LogEntry
}

type LogEntry struct {
	term int
	item interface{}
}

func (r *RaftLog) AppendEntries(prevIndex int, prevTerm int, newEntries []LogEntry) bool {
	// 2. Reply false if log doesn’t contain an entry at prevLogIndex ...
	if prevIndex >= len(r.entries) {
		return false
	}

	// ...whose term matches prevLogTerm (§5.3)
	if prevIndex != -1 {
		// -1 indicates no previous item, want to insert at the beginning of the log
		prevItem := r.entries[prevIndex]
		if prevItem.term != prevTerm {
			return false
		}
	}

	// 3. If an existing entry conflicts with a new one (same index
	// but different terms), delete the existing entry and all that
	// follow it (§5.3)
	// (in the common case of appending to the end of the array,
	// len(entries) == newIndexStart, and the loop will run zero times)
	newIndexStart := prevIndex + 1
	newIndexEnd := prevIndex + len(newEntries) + 1
	numToCheck := utils.Min(len(newEntries), len(r.entries)-newIndexStart)
	for i := 0; i < numToCheck; i++ {
		currentEntry := r.entries[newIndexStart+i]
		newEntry := newEntries[i]
		log.Printf("i = %v; currentEntry = %v; newEntry = %v\n", i, currentEntry, newEntry)
		// If the terms differ, we must replace with what we're given. Even if the
		// one we have has a newer term -- which has to be possible, eg we match at
		// the start but our current entries increment part-way through due to
		// getting them from some different leader. But it doesn't matter, it's for
		// the leader to solve that, not us. Our job is to replicate exactly what
		// our current leader says. cf raft.tla#L377
		if currentEntry.term != newEntry.term {
			log.Printf("Terms compared unequal, truncating entries back to %v", newIndexStart+i)
			r.entries = r.entries[:newIndexStart+i]
			break
		}
	}

	// If the new entries to be added are entirely within the current entries,
	// nothing more to do -- the term matches, so we're guaranteed that the
	// entries will match.
	if newIndexEnd < len(r.entries) {
		return true
	}

	// 4. Append any new entries not already in the log. Can start from the end of current entries
	firstActuallyNewEntryIndex := len(r.entries) - newIndexStart
	for i := firstActuallyNewEntryIndex; i < len(newEntries); i++ {
		r.entries = append(r.entries, newEntries[i])
	}

	// 5. If leaderCommit > commitIndex, set commitIndex =
	// min(leaderCommit, index of last new entry)
	// TODO
	return true
}

func stringEntry(entry LogEntry) string {
	return fmt.Sprintf("'%v' (%d)", entry.item, entry.term)
}

func (r *RaftLog) String() string {
	return fmt.Sprintf(strings.Join(utils.Map(r.entries, stringEntry), ", "))
}
