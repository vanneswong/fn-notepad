// Package store implements a small, safe, filesystem-backed note store.
//
// Every note is a single plain-text file inside one directory. All access is
// funnelled through this package so that callers never have to reason about
// path traversal, atomic writes, or naming rules themselves.
package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

// ErrInvalidName is returned when a caller supplies a name that cannot be
// turned into a safe single path segment.
var ErrInvalidName = errors.New("invalid note name")

// ErrNotFound is returned when a note does not exist on disk.
var ErrNotFound = errors.New("note not found")

const (
	maxNameRunes = 120
	defaultExt   = ".txt"
)

// Note is the metadata view of a note. Content is intentionally not included
// so that listing many notes stays cheap.
type Note struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"modTime"` // unix milliseconds
}

// Store owns a single directory of notes.
type Store struct {
	dir string
	mu  sync.RWMutex
}

// New creates the directory (if needed) and returns a Store rooted at it.
func New(dir string) (*Store, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("create notes dir: %w", err)
	}
	return &Store{dir: abs}, nil
}

// Dir reports the absolute directory backing the store.
func (s *Store) Dir() string { return s.dir }

// List returns every note, newest first.
func (s *Store) List() ([]Note, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	notes := make([]Note, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		notes = append(notes, Note{
			Name:    e.Name(),
			Size:    info.Size(),
			ModTime: info.ModTime().UnixMilli(),
		})
	}
	sort.Slice(notes, func(i, j int) bool {
		if notes[i].ModTime == notes[j].ModTime {
			return strings.ToLower(notes[i].Name) < strings.ToLower(notes[j].Name)
		}
		return notes[i].ModTime > notes[j].ModTime
	})
	return notes, nil
}

// Read returns the full text content of a note.
func (s *Store) Read(name string) (Note, string, error) {
	clean, err := NormalizeName(name)
	if err != nil {
		return Note{}, "", err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	path := s.path(clean)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Note{}, "", ErrNotFound
		}
		return Note{}, "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Note{}, "", err
	}
	note := Note{Name: clean, Size: info.Size(), ModTime: info.ModTime().UnixMilli()}
	return note, string(data), nil
}

// Write creates or replaces a note atomically and returns its new metadata.
func (s *Store) Write(name, content string) (Note, error) {
	clean, err := NormalizeName(name)
	if err != nil {
		return Note{}, err
	}
	if !utf8.ValidString(content) {
		return Note{}, fmt.Errorf("content is not valid UTF-8")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.path(clean)
	if err := atomicWrite(path, []byte(content)); err != nil {
		return Note{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return Note{}, err
	}
	return Note{Name: clean, Size: info.Size(), ModTime: info.ModTime().UnixMilli()}, nil
}

// Create makes a new, empty note. It fails if the note already exists.
func (s *Store) Create(name string) (Note, error) {
	clean, err := NormalizeName(name)
	if err != nil {
		return Note{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.path(clean)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return Note{}, fmt.Errorf("%s already exists", clean)
		}
		return Note{}, err
	}
	if err := f.Close(); err != nil {
		return Note{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return Note{}, err
	}
	return Note{Name: clean, Size: 0, ModTime: info.ModTime().UnixMilli()}, nil
}

// Delete removes a note.
func (s *Store) Delete(name string) error {
	clean, err := NormalizeName(name)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.path(clean)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return err
	}
	return os.Remove(path)
}

// Rename moves a note to a new name, refusing to overwrite an existing file.
func (s *Store) Rename(oldName, newName string) (Note, error) {
	oldClean, err := NormalizeName(oldName)
	if err != nil {
		return Note{}, err
	}
	newClean, err := NormalizeName(newName)
	if err != nil {
		return Note{}, err
	}
	if oldClean == newClean {
		note, _, err := s.Read(oldClean)
		return note, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	src := s.path(oldClean)
	dst := s.path(newClean)
	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return Note{}, ErrNotFound
		}
		return Note{}, err
	}
	if _, err := os.Stat(dst); err == nil {
		return Note{}, fmt.Errorf("%s already exists", newClean)
	}
	if err := os.Rename(src, dst); err != nil {
		return Note{}, err
	}
	info, err := os.Stat(dst)
	if err != nil {
		return Note{}, err
	}
	return Note{Name: newClean, Size: info.Size(), ModTime: info.ModTime().UnixMilli()}, nil
}

// path joins a already-normalised name onto the store directory.
func (s *Store) path(name string) string {
	return filepath.Join(s.dir, name)
}

// NormalizeName validates a user-supplied note name and guarantees the result
// is a single, safe path segment. Names without an extension get ".txt".
func NormalizeName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	name = strings.TrimSuffix(name, "/")
	if name == "" {
		return "", fmt.Errorf("%w: name is empty", ErrInvalidName)
	}
	if utf8.RuneCountInString(name) > maxNameRunes {
		return "", fmt.Errorf("%w: name is too long", ErrInvalidName)
	}
	if name == "." || name == ".." || strings.Contains(name, "..") {
		return "", fmt.Errorf("%w: name may not contain \"..\"", ErrInvalidName)
	}
	if strings.ContainsAny(name, `/\`) || strings.ContainsRune(name, 0) {
		return "", fmt.Errorf("%w: name may not contain path separators", ErrInvalidName)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("%w: name may not contain control characters", ErrInvalidName)
		}
	}
	if strings.HasPrefix(name, ".") {
		return "", fmt.Errorf("%w: name may not start with a dot", ErrInvalidName)
	}
	if filepath.Ext(name) == "" {
		name += defaultExt
	}
	return name, nil
}

// atomicWrite writes data to path via a temp file + rename so that a crash
// mid-save can never leave a half-written note behind.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if we bail out early.
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	tmpName = "" // rename succeeded, nothing to clean up
	return nil
}
