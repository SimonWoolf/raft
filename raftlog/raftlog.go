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
	numToCheck := utils.Min(len(newEntries), len(r.entries)-newIndexStart)
	for i := 0; i < numToCheck; i++ {
		currentEntry := r.entries[newIndexStart+i]
		newEntry := newEntries[i]
		log.Printf("i = %v; currentEntry = %v; newEntry = %v\n", i, currentEntry, newEntry)
		// note that if the terms differ, the newEntry must be newer. TODO wait no
		// that's not true though... we could match at the start but our current
		// entries increment part way through due to a now-fixed partition or
		// whatever. What do we do if the newEntry has an older term than we
		// currently have? need to check the formal spec
		if currentEntry.term != newEntry.term {
			log.Printf("Terms compared unequal, truncating entries back to %v", newIndexStart+i)
			r.entries = r.entries[:newIndexStart+i]
			break
		}
	}

	// 4. Append any new entries not already in the log. Actually for now just
	// set everything unconditionally; TODO do that optimization
	for i := 0; i <= len(newEntries); i++ {
		if i+newIndexStart < len(r.entries) {
			r.entries[i+newIndexStart] = newEntries[i]
		} else {
			r.entries = append(r.entries, newEntries[i])
		}
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
