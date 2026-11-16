package tgclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/gotd/td/telegram/peers"
	"github.com/nphuoctho/telegram-cli/internal/config"
)

// PeerStore is a peers.Storage persisted to peers.json (access hashes only).
// Phones and the contacts hash stay in memory.
type PeerStore struct {
	peers.InmemoryStorage // phones and contacts hash

	path  string
	mu    sync.Mutex
	data  map[peers.Key]int64
	dirty bool
}

var _ peers.Storage = (*PeerStore)(nil)

// PeersPath returns <dataDir>/peers.json.
func PeersPath(dataDir string) string { return filepath.Join(dataDir, "peers.json") }

// LoadPeerStore reads path; a missing file is an empty store, a corrupt one is an error.
func LoadPeerStore(path string) (*PeerStore, error) {
	s := &PeerStore{path: path, data: map[peers.Key]int64{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var raw map[string]int64
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("%s is corrupt (%v); delete it to rebuild the peer cache", path, err)
	}
	for k, v := range raw {
		i := strings.LastIndexByte(k, '_')
		id, perr := strconv.ParseInt(k[i+1:], 10, 64)
		if i < 0 || perr != nil {
			return nil, fmt.Errorf("%s is corrupt (bad key %q); delete it to rebuild the peer cache", path, k)
		}
		s.data[peers.Key{Prefix: k[:i+1], ID: id}] = v
	}
	return s, nil
}

// Save implements peers.Storage.
func (s *PeerStore) Save(_ context.Context, key peers.Key, v peers.Value) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.data[key]; !ok || old != v.AccessHash {
		s.data[key] = v.AccessHash
		s.dirty = true
	}
	return nil
}

// Find implements peers.Storage.
func (s *PeerStore) Find(_ context.Context, key peers.Key) (peers.Value, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.data[key]
	return peers.Value{AccessHash: h}, ok, nil
}

// FindPhone implements peers.Storage using the in-memory phone map and the persisted hashes.
func (s *PeerStore) FindPhone(ctx context.Context, phone string) (peers.Key, peers.Value, bool, error) {
	key, _, ok, err := s.InmemoryStorage.FindPhone(ctx, phone)
	if err != nil || !ok {
		return peers.Key{}, peers.Value{}, false, err
	}
	v, ok, err := s.Find(ctx, key)
	return key, v, ok, err
}

// Flush writes the file if anything changed.
func (s *PeerStore) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirty {
		return nil
	}
	raw := make(map[string]int64, len(s.data))
	for k, v := range s.data {
		raw[k.Prefix+strconv.FormatInt(k.ID, 10)] = v
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	if err := config.WriteFileAtomic(s.path, b); err != nil {
		return err
	}
	s.dirty = false
	return nil
}
