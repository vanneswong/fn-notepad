package oplog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAppendAndList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "oplog.jsonl")
	l, err := New(path)
	if err != nil {
		t.Fatal(err)
	}

	// 还没有任何记录时返回空列表，而不是报错。
	entries, err := l.List(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("empty log = %d entries", len(entries))
	}

	if err := l.Append("新建笔记", "admin", "日记.txt"); err != nil {
		t.Fatal(err)
	}
	if err := l.Append("删除笔记", "admin", "日记.txt"); err != nil {
		t.Fatal(err)
	}
	if err := l.Append("登录", "admin", ""); err != nil {
		t.Fatal(err)
	}

	entries, err = l.List(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("len = %d, want 3", len(entries))
	}
	// 最新的在最前面。
	if entries[0].Action != "登录" || entries[1].Action != "删除笔记" || entries[2].Action != "新建笔记" {
		t.Fatalf("order = %+v", entries)
	}
	if entries[0].User != "admin" {
		t.Fatalf("user = %q", entries[0].User)
	}

	// limit 裁剪保留最新的几条。
	entries, err = l.List(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Action != "登录" || entries[1].Action != "删除笔记" {
		t.Fatalf("limited = %+v", entries)
	}

	// 日志文件必须是私有的，避免账号名外泄。
	info, err := os.Stat(l.Path())
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mode = %o, want 600", perm)
	}
}
