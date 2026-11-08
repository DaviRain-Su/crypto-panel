package plugin

import (
	"sync"
	"time"
)

// LogEntry is one line a plugin logged, or one the host logged about it.
type LogEntry struct {
	Time    time.Time
	Level   string
	Message string
}

// LogBuffer keeps the last lines of one plugin's log, for the manager. It
// is safe for concurrent use: host functions log from the runtime's
// goroutine, the manager reads from the main thread.
type LogBuffer struct {
	mu      sync.Mutex
	entries []LogEntry
	max     int
}

// NewLogBuffer returns a buffer of at most max entries.
func NewLogBuffer(max int) *LogBuffer {
	if max <= 0 {
		max = 100
	}
	return &LogBuffer{max: max}
}

// Add appends one line, dropping the oldest when full.
func (b *LogBuffer) Add(level, msg string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.entries) == b.max {
		copy(b.entries, b.entries[1:])
		b.entries = b.entries[:len(b.entries)-1]
	}
	b.entries = append(b.entries, LogEntry{Time: time.Now(), Level: level, Message: msg})
}

// Entries returns a copy of the lines, oldest first.
func (b *LogBuffer) Entries() []LogEntry {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]LogEntry(nil), b.entries...)
}

// Clear drops every line.
func (b *LogBuffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.entries = nil
}
