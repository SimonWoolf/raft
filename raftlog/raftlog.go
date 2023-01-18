package raftlog

import (
	"fmt"
	"log"
	"raft/raftrpc"
	"raft/utils"
	"strings"
)

type LogEntry *raftrpc.LogEntry // this has to be a *LogEntry and not a LogEntry
// because grpc-go for some reason shoves a mutex in the generated proto message
// definition, which loudly whines if it's ever copied

type RaftLog struct {
	Entries []LogEntry
}

type Index = int32
type Term = int32

func MakeLogEntry(term int, item string) LogEntry {
	return &raftrpc.LogEntry{Term: Term(term), Item: item}
}

func (r *RaftLog) AppendEntries(prevIndex Index, prevTerm Term, newEntries []LogEntry) bool {
	// 2. Reply false if log doesn’t contain an entry at prevLogIndex ...
	if int(prevIndex) >= len(r.Entries) {
		return false
	}

	// ...whose term matches prevLogTerm (§5.3)
	if prevIndex != -1 {
		// -1 indicates no previous item, want to insert at the beginning of the log
		prevItem := r.Entries[prevIndex]
		if prevItem.Term != prevTerm {
			return false
		}
	}

	// 3. If an existing entry conflicts with a new one (same index
	// but different terms), delete the existing entry and all that
	// follow it (§5.3)
	// (in the common case of appending to the end of the array,
	// len(entries) == newIndexStart, and the loop will run zero times)
	newIndexStart := prevIndex + 1
	newIndexEnd := prevIndex + Index(len(newEntries)) + 1
	numToCheck := utils.Min(len(newEntries), len(r.Entries)-int(newIndexStart))
	for i := 0; i < numToCheck; i++ {
		currentEntry := r.Entries[int(newIndexStart)+i]
		newEntry := newEntries[i]
		log.Printf("i = %v; currentEntry = %v; newEntry = %v\n", i, currentEntry, newEntry)
		// If the terms differ, we must replace with what we're given. Even if the
		// one we have has a newer term -- which has to be possible, eg we match at
		// the start but our current entries increment part-way through due to
		// getting them from some different leader. But it doesn't matter, it's for
		// the leader to solve that, not us. Our job is to replicate exactly what
		// our current leader says. cf raft.tla#L377
		if currentEntry.Term != newEntry.Term {
			log.Printf("Terms compared unequal, truncating entries back to %v", int(newIndexStart)+i)
			r.Entries = r.Entries[:int(newIndexStart)+i]
			break
		}
	}

	// If the new entries to be added are entirely within the current entries,
	// nothing more to do -- the term matches, so we're guaranteed that the
	// entries will match.
	if int(newIndexEnd) < len(r.Entries) {
		return true
	}

	// 4. Append any new entries not already in the log. Can start from the end of current entries
	firstActuallyNewEntryIndex := len(r.Entries) - int(newIndexStart)
	for i := firstActuallyNewEntryIndex; i < len(newEntries); i++ {
		r.Entries = append(r.Entries, newEntries[i])
	}

	// 5. If leaderCommit > commitIndex, set commitIndex =
	// min(leaderCommit, index of last new entry)
	// TODO
	return true
}

func stringEntry(entry LogEntry) string {
	return fmt.Sprintf("'%v' (%d)", entry.Item, entry.Term)
}

func (r *RaftLog) String() string {
	return fmt.Sprintf(strings.Join(utils.Map(r.Entries, stringEntry), ", "))
}
