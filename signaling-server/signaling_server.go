package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)


func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}


var store *Store


// one flat registry for the whole server
var clients = struct {
	mu sync.Mutex
	registry map[string]*Client
}{registry: make(map[string]*Client)}



func registerClient(client *Client) {
	clients.mu.Lock()
	defer clients.mu.Unlock()
	clients.registry[client.id] = client
}

func unregisterClient(id string) {
	clients.mu.Lock()
	defer clients.mu.Unlock()
	delete(clients.registry, id)
}

func lookupClient(id string) (*Client, bool) {
	clients.mu.Lock()
	defer clients.mu.Unlock()
	c, ok := clients.registry[id]
	return c, ok
}

func (client *Client) writeLoop() {
	defer close(client.writerDone)
	for msg := range client.sendQueue {
		log.Println("writing to network:", msg.Type)
		err := client.conn.WriteJSON(msg)
		if err != nil { log.Println("write error:", err); return }
	}
}


func handleWS(w http.ResponseWriter, r *http.Request) {
	// authenticate via session token
	token := r.URL.Query().Get("token")
	if token == "" { http.Error(w, "missing token param", http.StatusUnauthorized); return }
	name, ok := store.LookupSession(token)
	if !ok { http.Error(w, "invalid or expired session", http.StatusUnauthorized); return }
	// read the room and client name from the query string
	roomID := r.URL.Query().Get("room")
	if roomID == "" { http.Error(w, "missing room param", http.StatusBadRequest); return }
	isMember, err := store.IsMember(roomID, name)
	if err != nil { http.Error(w, "internal error", http.StatusInternalServerError); return }
	if !isMember { http.Error(w, "not a member of this room", http.StatusForbidden); return }
	// ---
	upgrader := websocket.Upgrader{ CheckOrigin: func(r *http.Request) bool { return true } }
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil { log.Println("upgrade error:", err); return }
	defer conn.Close()
	// create and register client
	client := &Client{
		id:			uuid.NewString(),
		name:		name,
		conn:		conn,
		sendQueue:	make(chan SignalMessage, 16),
		writerDone: make(chan struct{}),
	}
	registerClient(client)
	// writer goroutine: owns conn.WriteJSON so writes never race with the reader
	go client.writeLoop() // <- !!! important - writes happen here
	// ---
	room := getOrCreateRoom(roomID)
	existingPeers := room.join(client)
	// ---
	// tell the client its own assigned ID (it needs this to give out to peers via invite
	log.Println("enqueuing welcome message")
	client.sendQueue <- SignalMessage {
		Type:	"welcome",
		From:	"server",
		Data:	mustJSON(map[string]any{"id": client.id, "name": client.name, "peers": existingPeers}),
	}
	// tell the existing room members someone new arrived
	room.broadcast(client.id, SignalMessage{ 
		Type: 	"peer-joined", 
		From: 	client.id,
		Data:	mustJSON(PeerInfo{ID: client.id, Name: client.name}),
	})
	// ---
	defer func() {
		despawnAllOwnedSynthetics(client, room, roomID)
		unregisterClient(client.id)
		room.leave(client.id)
		room.broadcast(client.id, SignalMessage{ Type: "peer-left", From: client.id })
		deleteRoomIfEmpty(roomID, room)
		close(client.sendQueue)
		<-client.writerDone
	}()
	// --- relay message
	for {
		var msg SignalMessage
		if err := conn.ReadJSON(&msg); err != nil { break }
		msg.From = client.id // never trust a client-claimed sender ID...
		if msg.As != "" {
			// ...unless it's signing as a synthetic identity it actually owns
			synth, ok := lookupClient(msg.As)
			if !ok || synth.ownerID != client.id {
				client.sendQueue <- SignalMessage{
					Type:	"error",
					From:	"server",
					Data:	mustJSON(map[string]string{"reason": "not authorized to act as: " + msg.As}),
				}
				continue
			}
			msg.From = msg.As
		}
		switch msg.Type {
		case "offer", "answer", "ice-candidate":
			target, ok := lookupClient(msg.To)
			if !ok {
				client.sendQueue <- SignalMessage{
					Type:	"error",
					From:	"server",
					Data:	mustJSON(map[string]string{"reason": "peer not found: " + msg.To}),
				}
				continue
			}
			target.sendQueue <- msg
		case "spawn-synthetic":
			var payload struct{ Name string `json:"name"` }
			json.Unmarshal(msg.Data, &payload)
			name := payload.Name
			if name == "" { name = "media" }
			synth, existingPeers := spawnSynthetic(client, room, name)
			client.sendQueue <- SignalMessage{
				Type:	"synthetic-welcome",
				From:	"server",
				Data:	mustJSON(map[string]any{"id": synth.id, "name": synth.name, "peers": existingPeers}),
			}
			room.broadcast(synth.id, SignalMessage{
				Type: "peer-joined",
				From: synth.id,
				Data: mustJSON(PeerInfo{ID: synth.id, Name: synth.name, Synthetic: true}),
			})
		case "despawn-synthetic":
			var payload struct{ ID string `json:"id"` }
			json.Unmarshal(msg.Data, &payload)
			if !despawnSynthetic(client, room, roomID, payload.ID) {
				client.sendQueue <- SignalMessage{
					Type:	"error",
					From:	"server",
					Data:	mustJSON(map[string]string{"reason": "not found or not owned: " + payload.ID}),
				}
			}
		case "roll-request":
			var payload struct{ Notation string `json:"notation"` }
			json.Unmarshal(msg.Data, &payload)
			count, sides, modifier, err := parseDiceNotation(payload.Notation)
			if err != nil {
				client.sendQueue <- SignalMessage{
					Type:	"error",
					From:	"server",
					Data:	mustJSON(map[string]string{"reason": "bad roll: " + err.Error()}),
				}
				continue
			}
			results := rollDice(count, sides)
			room.broadcast("", SignalMessage{
				Type: "roll-result",
				From: "server",
				Data: mustJSON(map[string]any{
					"roller":   client.name,
					"notation": payload.Notation,
					"results":  results,
					"total":    sumInts(results) + modifier,
				}),
			})
		default:
			log.Println("unknown message type:", msg.Type)
		}
	}
}


func main() {
	s, err := NewStore("skulls.db")
	if err != nil { log.Fatal("store init error:", err) }
	store = s

	http.HandleFunc("/api/register", 	rateLimited(handleRegister))
	http.HandleFunc("/api/login", 		rateLimited(handleLogin))
	http.HandleFunc("/api/rooms", 		handleRooms)
	http.HandleFunc("/api/rooms/",		handleRoomsSubroutes)
	http.HandleFunc("/api/invites/", 	handleAcceptInvite)
	http.HandleFunc("/api/account", 	handleDeleteAccount)
	http.HandleFunc("/ws", 				handleWS)
	
	log.Println("signaling server on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}