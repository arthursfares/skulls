package main

import (
	"sync"

	"github.com/google/uuid"
)

// synthOwners tracks which real client owns which synthetic client IDs, so
// the relay can validate a message's "as" field and so a disconnecting
// owner's synthetics can be found and despawned.
var synthOwners = struct {
	mu      sync.Mutex
	byOwner map[string][]string
}{byOwner: make(map[string][]string)}

func trackSynthetic(ownerID, synthID string) {
	synthOwners.mu.Lock()
	defer synthOwners.mu.Unlock()
	synthOwners.byOwner[ownerID] = append(synthOwners.byOwner[ownerID], synthID)
}

func untrackSynthetic(ownerID, synthID string) {
	synthOwners.mu.Lock()
	defer synthOwners.mu.Unlock()
	ids := synthOwners.byOwner[ownerID]
	for i, id := range ids {
		if id == synthID {
			synthOwners.byOwner[ownerID] = append(ids[:i], ids[i+1:]...)
			break
		}
	}
	if len(synthOwners.byOwner[ownerID]) == 0 { delete(synthOwners.byOwner, ownerID) }
}

// NOTE. syntheticsOwnedBy returns a copy of the synthetic IDs currently owned by
// ownerID, safe to range over after releasing the lock.
func syntheticsOwnedBy(ownerID string) []string {
	synthOwners.mu.Lock()
	defer synthOwners.mu.Unlock()
	ids := synthOwners.byOwner[ownerID]
	out := make([]string, len(ids))
	copy(out, ids)
	return out
}

// NOTE. spawnSynthetic registers a new synthetic Client whose sendQueue is
// aliased to owner's real queue - so anything routed "to" it is delivered
// over the owner's actual websocket.
func spawnSynthetic(owner *Client, room *Room, name string) (*Client, []PeerInfo) {
	synth := &Client{
		id:        uuid.NewString(),
		name:      name,
		conn:      nil,
		sendQueue: owner.sendQueue,
		ownerID:   owner.id,
	}
	registerClient(synth)
	existingPeers := room.join(synth)
	trackSynthetic(owner.id, synth.id)
	// owner is already in room.clients (they joined it as themselves before
	// ever spawning a synthetic), so room.join's "existing" list includes
	// them - filter that out, or the owner ends up calling its own
	// synthetic's offer back to itself.
	callablePeers := existingPeers[:0]
	for _, p := range existingPeers {
		if p.ID == owner.id { continue }
		callablePeers = append(callablePeers, p)
	}
	return synth, callablePeers
}

// NOTE. despawnSynthetic validates owner actually owns synthID, then removes it
// from the room and global registry and broadcasts peer-left.
func despawnSynthetic(owner *Client, room *Room, roomID string, synthID string) bool {
	synth, ok := lookupClient(synthID)
	if !ok || synth.ownerID != owner.id { return false }
	unregisterClient(synthID)
	room.leave(synthID)
	room.broadcast(synthID, SignalMessage{Type: "peer-left", From: synthID})
	deleteRoomIfEmpty(roomID, room)
	untrackSynthetic(owner.id, synthID)
	return true
}

func despawnAllOwnedSynthetics(owner *Client, room *Room, roomID string) {
	for _, id := range syntheticsOwnedBy(owner.id) {
		despawnSynthetic(owner, room, roomID, id)
	}
}
