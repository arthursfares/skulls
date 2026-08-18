package main

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/time/rate"
	"gorm.io/gorm"
)

// SignalMessage is the envelope for everything sent over the socket.
type SignalMessage struct {
	Type 			string          		`json:"type"`           // "offer", "answer", "ice-candidate", "welcome", "error", "roll-request", "roll-result", "spawn-synthetic", "synthetic-welcome", "despawn-synthetic".
	From 			string         			`json:"from,omitempty"` // sender's peer ID (server fills this in)
	To   			string          		`json:"to,omitempty"`   // target peer ID (required for offer/answer/ice-candidate)
	As   			string          		`json:"as,omitempty"`   // sign this message as a synthetic identity the sender owns, instead of their own ID
	Data 			json.RawMessage 		`json:"data,omitempty"` // SDP or ICE candidate payload
}

type Client struct {
	id				string
	name			string
	conn			*websocket.Conn 		// nil for synthetic clients - sendQueue is aliased to the owner's real connection instead
	sendQueue		chan SignalMessage
	writerDone 		chan struct{}
	ownerID			string 					// empty for real clients; owning client's id for synthetic ones
}

type PeerInfo struct {
	ID				string					`json:"id"`
	Name			string					`json:"name"`
	Synthetic		bool					`json:"synthetic,omitempty"`
}

type Room struct {
	mu				sync.Mutex
	clients			map[string]*Client
}

type RoomSummary struct {
	ID    			string 					`json:"id"`
	Name  			string 					`json:"name"`
	Owner 			string 					`json:"owner"`
}
 
type InviteSummary struct {
	RoomID   		string 					`json:"roomId"`
	RoomName 		string 					`json:"roomName"`
	From     		string					`json:"from"`
}

// RoomMembersResponse is served by GET /api/rooms/{roomID}/members
type RoomMembersResponse struct {
	Owner          	string   				`json:"owner"`
	Members        	[]string 				`json:"members"`
	PendingInvites 	[]string 				`json:"pendingInvites,omitempty"`
}

type Store struct {
	db				*gorm.DB
	sessions 		map[string]string 		// token -> username
	sessionsMu      sync.Mutex
}
 
type RoomRecord struct {
	ID        		string    				`json:"id"`
	Name      		string    				`json:"name"`
	Owner     		string    				`json:"owner"`
	Members   		[]string  				`json:"members"` 		 // owner + everyone who accepted an invite
	CreatedAt 		time.Time 				`json:"createdAt"`
}
 
type InviteRecord struct {
	RoomID    		string    				`json:"roomId"`
	RoomName  		string    				`json:"roomName"`
	From      		string    				`json:"from"`
	To        		string    				`json:"to"`
	CreatedAt 		time.Time 				`json:"createdAt"`
}

type ipLimiter struct {
	mu				sync.Mutex
	limiters		map[string]*rate.Limiter
}