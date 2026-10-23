package room

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"math/big"
	"sync"
)

const (
	idChars  = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	idLength = 6
)

var ErrRoomNotFound = errors.New("room not found")
var ErrRoomExists = errors.New("room already exists")

// Registry maps room IDs to rooms.
type Registry struct {
	mu    sync.RWMutex
	rooms map[string]*Room
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{rooms: make(map[string]*Room)}
}

// Create generates a unique room ID, creates a room, and registers it.
func (rg *Registry) Create() (*Room, error) {
	token, err := generateToken()
	if err != nil {
		return nil, err
	}

	for range 10 { // retry on collision
		id, err := generateID()
		if err != nil {
			return nil, err
		}
		rg.mu.Lock()
		if _, exists := rg.rooms[id]; !exists {
			r := New(id, token)
			rg.rooms[id] = r
			rg.mu.Unlock()
			return r, nil
		}
		rg.mu.Unlock()
	}
	return nil, errors.New("failed to generate unique room ID after 10 attempts")
}

// Get returns the room for the given ID, or ErrRoomNotFound.
func (rg *Registry) Get(id string) (*Room, error) {
	rg.mu.RLock()
	defer rg.mu.RUnlock()
	r, ok := rg.rooms[id]
	if !ok {
		return nil, ErrRoomNotFound
	}
	return r, nil
}

// Delete removes a room from the registry.
func (rg *Registry) Delete(id string) {
	rg.mu.Lock()
	defer rg.mu.Unlock()
	delete(rg.rooms, id)
}

// generateID returns a 6-char cryptographically random A-Z0-9 string.
func generateID() (string, error) {
	b := make([]byte, idLength)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(idChars))))
		if err != nil {
			return "", err
		}
		b[i] = idChars[n.Int64()]
	}
	return string(b), nil
}

// generateToken returns a 32-byte hex token.
func generateToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// generateParticipantID returns a short hex ID for a participant.
func generateParticipantID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// NewParticipantID is exported for use by the realtime package.
func NewParticipantID() (string, error) {
	return generateParticipantID()
}
