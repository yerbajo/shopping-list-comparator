// Package store keeps the shopping list on disk as one JSON document.
//
// A list is small — a few dozen items with their equivalents — and a single user owns it,
// so a file with an atomic write (temp file plus rename) is enough and needs no database,
// no driver and no migration. The upgrade path, if this ever serves several users, is a
// real store behind this same interface.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/yerbajo/shopping-list-comparator/internal/identity"
)

// ErrEmptyQuery is what Add rejects: a list item is a thing, not whitespace.
var ErrEmptyQuery = errors.New("el producto no puede estar vacío")

// Item is one thing the shopper wants. Anchor is the product added first; Matches holds the
// best equivalent per chain; Candidates keeps the alternatives so a shopper can overrule
// the machine without spending another search.
type Item struct {
	ID    string `json:"id"`
	Query string `json:"query"`
	// Quantity is how many of this thing the shopper wants; Section is the aisle it is filed
	// under. Both are the shopper's, never the engine's.
	Quantity   int                `json:"quantity"`
	Section    string             `json:"section,omitempty"`
	Anchor     *identity.Product  `json:"anchor,omitempty"`
	Matches    []identity.Match   `json:"matches,omitempty"`
	Candidates []identity.Product `json:"candidates,omitempty"`
	// Status is each chain's state for this item: "" when it answered, or the engine's own
	// code (quota, blocked, timeout, network, parse) when it did not.
	Status     map[string]string `json:"status,omitempty"`
	ResolvedAt *time.Time        `json:"resolvedAt,omitempty"`
	Resolving  bool              `json:"resolving,omitempty"`
	Error      string            `json:"error,omitempty"`
}

// State is the whole document.
type State struct {
	Items []Item `json:"items"`
}

// Store is the list, safe for concurrent use.
type Store struct {
	path  string
	mu    sync.Mutex
	state State
}

// Open loads the list at path, creating nothing until the first write.
func Open(path string) (*Store, error) {
	s := &Store{path: path}
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &s.state); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	case errors.Is(err, os.ErrNotExist):
	default:
		return nil, err
	}
	// A resolution that was running when the process stopped is not running now: an item
	// left `resolving` forever would hide the retry button.
	for i := range s.state.Items {
		s.state.Items[i].Resolving = false
		if s.state.Items[i].Quantity < 1 {
			s.state.Items[i].Quantity = 1
		}
	}
	return s, nil
}

// Path reports where the list is stored.
func (s *Store) Path() string { return s.path }

// Snapshot returns a copy of the document.
func (s *Store) Snapshot() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := State{Items: make([]Item, len(s.state.Items))}
	copy(out.Items, s.state.Items)
	return out
}

// Get returns one item.
func (s *Store) Get(id string) (Item, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, it := range s.state.Items {
		if it.ID == id {
			return it, true
		}
	}
	return Item{}, false
}

// Add appends an item with a fresh random id and returns it.
func (s *Store) Add(query string) (Item, error) {
	query = strings.Join(strings.Fields(query), " ")
	if query == "" {
		return Item{}, ErrEmptyQuery
	}
	id, err := newID()
	if err != nil {
		return Item{}, err
	}
	it := Item{ID: id, Query: query, Quantity: 1, Resolving: true}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Items = append(s.state.Items, it)
	if err := s.saveLocked(); err != nil {
		s.state.Items = s.state.Items[:len(s.state.Items)-1]
		return Item{}, err
	}
	return it, nil
}

// Remove deletes one item.
func (s *Store) Remove(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, it := range s.state.Items {
		if it.ID != id {
			continue
		}
		s.state.Items = append(s.state.Items[:i], s.state.Items[i+1:]...)
		return true, s.saveLocked()
	}
	return false, nil
}

// Update mutates one item and persists the result.
func (s *Store) Update(id string, fn func(*Item)) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.state.Items {
		if s.state.Items[i].ID != id {
			continue
		}
		fn(&s.state.Items[i])
		return true, s.saveLocked()
	}
	return false, nil
}

func (s *Store) saveLocked() error {
	if dir := filepath.Dir(s.path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	raw, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func newID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
