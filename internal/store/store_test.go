package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeName(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"note", "note.txt", false},
		{"note.txt", "note.txt", false},
		{"  空闲 记录.md ", "空闲 记录.md", false},
		{"会议/记录", "", true},
		{"..", "", true},
		{"../../etc/passwd", "", true},
		{"", "", true},
		{".hidden", "", true},
		{"bad\x00name", "", true},
		{"a\nb", "", true},
	}
	for _, c := range cases {
		got, err := NormalizeName(c.in)
		if c.wantErr {
			if !errors.Is(err, ErrInvalidName) {
				t.Errorf("NormalizeName(%q) error = %v, want ErrInvalidName", c.in, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("NormalizeName(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("NormalizeName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCRUD(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}

	note, err := s.Create("购物清单")
	if err != nil {
		t.Fatal(err)
	}
	if note.Name != "购物清单.txt" {
		t.Fatalf("created name = %q, want 购物清单.txt", note.Name)
	}

	if _, err := s.Create("购物清单"); err == nil {
		t.Fatal("expected duplicate create to fail")
	}

	if _, err := s.Write(note.Name, "牛奶\n面包\n"); err != nil {
		t.Fatal(err)
	}

	_, content, err := s.Read(note.Name)
	if err != nil {
		t.Fatal(err)
	}
	if content != "牛奶\n面包\n" {
		t.Fatalf("content = %q", content)
	}

	notes, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 {
		t.Fatalf("list len = %d, want 1", len(notes))
	}

	renamed, err := s.Rename(note.Name, "清单")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Name != "清单.txt" {
		t.Fatalf("renamed = %q", renamed.Name)
	}
	if _, err := os.Stat(filepath.Join(dir, "购物清单.txt")); !os.IsNotExist(err) {
		t.Fatalf("old file still present: %v", err)
	}

	if err := s.Delete("清单"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Read("清单.txt"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("read deleted = %v, want ErrNotFound", err)
	}
}

func TestRejectsTraversal(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Read("../../etc/passwd"); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("traversal read error = %v, want ErrInvalidName", err)
	}
	if _, err := s.Write("../escape.txt", "x"); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("traversal write error = %v, want ErrInvalidName", err)
	}
}

func TestWriteIsAtomicAndLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write("a.txt", "hello"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "a.txt" {
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("temp files left behind: %v", names)
	}
}
