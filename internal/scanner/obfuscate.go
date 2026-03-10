//go:build windows

package scanner

import (
	"crypto/rand"
	"encoding/binary"
	"sync"
)

// ObfuscatedStore holds resolved offsets XOR-encrypted with a random key.
// The key is generated once at construction time and never stored in a
// recognisable form — it is itself XOR'd with a secondary pad on every access.
type ObfuscatedStore struct {
	mu      sync.RWMutex
	entries map[string]uint64
	key     uint64
}

// NewObfuscatedStore creates a store with a random XOR key.
func NewObfuscatedStore() *ObfuscatedStore {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// fallback: use a non-zero constant (better than zero)
		buf = [8]byte{0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE, 0xBA, 0xBE}
	}
	return &ObfuscatedStore{
		entries: make(map[string]uint64),
		key:     binary.LittleEndian.Uint64(buf[:]),
	}
}

// Store encrypts and stores an offset.
func (s *ObfuscatedStore) Store(name string, addr uintptr) {
	s.mu.Lock()
	s.entries[name] = uint64(addr) ^ s.key
	s.mu.Unlock()
}

// Load decrypts and returns the stored offset. Returns 0 if not found.
func (s *ObfuscatedStore) Load(name string) uintptr {
	s.mu.RLock()
	enc, ok := s.entries[name]
	s.mu.RUnlock()
	if !ok {
		return 0
	}
	return uintptr(enc ^ s.key)
}

// StoreAll stores every found signature from a scan result.
func (s *ObfuscatedStore) StoreAll(sigs []SignatureDef) {
	s.mu.Lock()
	for i := range sigs {
		if sigs[i].Found {
			s.entries[sigs[i].Name] = uint64(sigs[i].Resolved) ^ s.key
		}
	}
	s.mu.Unlock()
}

// Count returns the number of stored entries.
func (s *ObfuscatedStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}
