// Package oplog implements a tiny append-only operation log. One JSON object
// per line, written to a single file next to users.json, so it survives app
// upgrades and uninstalls just like the notes and the account database.
package oplog

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Entry is a single operation record.
type Entry struct {
	Time   int64  `json:"time"`   // unix milliseconds
	Action string `json:"action"` // 操作名称，例如「新建笔记」
	User   string `json:"user"`   // 触发操作的账号
	Detail string `json:"detail"` // 具体对象，例如笔记名
}

// Log is a small, thread-safe JSONL log file.
type Log struct {
	path string
	mu   sync.Mutex
}

// New creates the parent directory (if needed) and returns a Log rooted at
// path. The file itself is created lazily on the first Append.
func New(path string) (*Log, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return nil, err
	}
	return &Log{path: abs}, nil
}

// Path reports the absolute path of the underlying log file.
func (l *Log) Path() string { return l.path }

// Append writes one entry to the end of the log file. The file is created
// with 0600 so account names never leak through group or world permissions.
func (l *Log) Append(action, user, detail string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	e := Entry{Time: time.Now().UnixMilli(), Action: action, User: user, Detail: detail}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := json.NewEncoder(f).Encode(e); err != nil {
		return err
	}
	return f.Sync()
}

// List returns the most recent entries, newest first. A limit of 0 or less
// means the default (200). A missing file yields an empty list, not an error,
// so a fresh install simply shows「暂无操作记录」.
func (l *Log) List(limit int) ([]Entry, error) {
	if limit <= 0 {
		limit = 200
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	f, err := os.Open(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []Entry{}, nil
		}
		return nil, err
	}
	defer f.Close()

	var all []Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			continue // 跳过损坏的行，不影响其余记录
		}
		all = append(all, e)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	if len(all) > limit {
		all = all[len(all)-limit:]
	}
	for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
		all[i], all[j] = all[j], all[i]
	}
	return all, nil
}
