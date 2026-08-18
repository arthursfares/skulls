package main

import (
	"log"
	"sync"
	"time"
)


var rooms = struct {
	sync.Mutex
	registry map[string]*Room
}{registry: make(map[string]*Room)}

func getOrCreateRoom(roomID string) *Room {
	rooms.Lock()
	defer rooms.Unlock()
	r, ok := rooms.registry[roomID]
	if !ok {
		r = &Room{clients: make(map[string]*Client)}
		rooms.registry[roomID] = r
	}
	return r
}

// NOTE. join adds the client to the room and returns the IDs of everyone
// already present, so the new arrival knows who to call
func (room *Room) join(newClient *Client) []PeerInfo {
	room.mu.Lock()
	defer room.mu.Unlock()
	existing := make([]PeerInfo, 0, len(room.clients))
	for id, c := range room.clients {
		existing = append(existing, PeerInfo{ID: id, Name: c.name, Synthetic: c.ownerID != ""})
	}
	room.clients[newClient.id] = newClient
	return existing
}

func (room *Room) leave(clientID string) {
	room.mu.Lock()
	defer room.mu.Unlock()
	delete(room.clients, clientID)
}

// NOTE. broadcast sends msg to every room member except exclude. A synthetic
// client's sendQueue is the same channel object as its owner's real one, so
// it's deduplicated here by channel identity - otherwise the owner of an
// active synthetic would get every broadcast delivered twice (once for
// their real Client entry, once for the synthetic's).
func (room *Room) broadcast(exclude string, msg SignalMessage) {
	room.mu.Lock()
	defer room.mu.Unlock()
	sent := make(map[chan SignalMessage]bool, len(room.clients))
	for id, client := range room.clients {
		if id == exclude { continue }
		if sent[client.sendQueue] { continue }
		sent[client.sendQueue] = true
		select {
		case client.sendQueue <- msg:
		default:
			log.Println("dropping room broadcast: slow client", id)
		}
	}
}

// NOTE. notifyRoomDeleted tells everyone currently connected to roomID that the 
// room is gone, then closes their connections shortly after so the notice 
// has a chance to flush.
func notifyRoomDeleted(roomID string) {
	room, ok := lookupRoom(roomID)
	if !ok { return }
	room.mu.Lock()
	sent := make(map[chan SignalMessage]bool, len(room.clients))
	for id, c := range room.clients {
		if sent[c.sendQueue] { continue }
		sent[c.sendQueue] = true
		select {
		case c.sendQueue <- SignalMessage{Type: "room-deleted", From: "server"}:
		default:
			log.Println("dropping room-deleted notice: slow client", id)
		}
	}
	room.mu.Unlock()
	go func(r *Room) {
		time.Sleep(200 * time.Millisecond)
		r.mu.Lock()
		for _, c := range r.clients {
			if c.conn != nil { c.conn.Close() } // synthetic clients have no real connection
		}
		r.mu.Unlock()
	}(room)
}

func lookupRoom(roomID string) (*Room, bool) {
	rooms.Lock()
	defer rooms.Unlock()
	r, ok := rooms.registry[roomID]
	return r, ok
}

func deleteRoomIfEmpty(roomID string, room *Room) {
	room.mu.Lock()
	empty := len(room.clients) == 0
	room.mu.Unlock()
	if empty {
		rooms.Lock()
		delete(rooms.registry, roomID)
		rooms.Unlock()
	}
}