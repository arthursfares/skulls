package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{ "error": msg })
}

// NOTE. authenticate pulls the session token out of the Authorization header 
// ("Bearer <token>") and resolves it to a username.
func authenticate(r *http.Request) (string, bool) {
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) { return "", false }
	token := strings.TrimPrefix(header, prefix)
	if token == "" { return "", false }
	return store.LookupSession(token)
}

func handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost { writeError(w, http.StatusMethodNotAllowed, "method not allowed"); return }
	var req struct{ 
		Username string 
		Password string 
	}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil { writeError(w, http.StatusBadRequest, "invalid request body"); return }
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" { writeError(w, http.StatusBadRequest, "username and password are required"); return }
	err = store.Register(req.Username, req.Password)
	if err != nil { writeError(w, http.StatusConflict, err.Error()); return }
	writeJSON(w, http.StatusCreated, map[string]string{ "status": "ok" })
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost { writeError(w, http.StatusMethodNotAllowed, "method not allowed"); return }
	var req struct{ 
		Username string 
		Password string 
	}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil { writeError(w, http.StatusBadRequest, "invalid request body"); return }
	username := strings.TrimSpace(req.Username)
	token, err := store.Login(username, req.Password)
	if err != nil { writeError(w, http.StatusUnauthorized, err.Error()); return }
	writeJSON(w, http.StatusOK, map[string]string{ "token": token, "username": username })
}

// NOTE. handleRooms serves GET (list rooms + invites) and POST (create a room)
func handleRooms(w http.ResponseWriter, r *http.Request) {
	username, ok := authenticate(r)
	if !ok { writeError(w, http.StatusUnauthorized, "invalid or missing session"); return }

	switch r.Method {
	case http.MethodGet:
		records, err := store.RoomsFor(username)
		if err != nil { writeError(w, http.StatusInternalServerError, "failed to load rooms"); return }
		rooms := make([]RoomSummary, 0, len(records))
		for _, rec := range records {
			rooms = append(rooms, RoomSummary{ ID: rec.ID, Name: rec.Name, Owner: rec.Owner })
		}
		invRecords, err := store.InvitesFor(username)
		if err != nil { writeError(w, http.StatusInternalServerError, "failed to load invites"); return }
		invites := make([]InviteSummary, 0, len(invRecords))
		for _, inv := range invRecords {
			invites = append(invites, InviteSummary{ RoomID: inv.RoomID, RoomName: inv.RoomName, From: inv.From })
		}
		writeJSON(w, http.StatusOK, map[string]any{ "rooms": rooms, "invites": invites })

	case http.MethodPost:
		var req struct{ Name string }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil { writeError(w, http.StatusBadRequest, "invalid request body"); return }
		req.Name = strings.TrimSpace(req.Name)
		if req.Name == "" { writeError(w, http.StatusBadRequest, "room name is required"); return }
		room, err := store.CreateRoom(req.Name, username)
		if err != nil {
			status := http.StatusInternalServerError
			if err == ErrRoomLimitReached || err == ErrRoomNameTaken { status = http.StatusConflict }
			writeError(w, status, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, RoomSummary{ ID: room.ID, Name: room.Name, Owner: room.Owner })

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func handleRoomsSubroutes(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/invite") { handleInvite(w, r); return }
	if strings.HasSuffix(r.URL.Path, "/members") { handleRoomMembers(w, r); return }
	handleDeleteRoom(w, r)
}

func handleDeleteRoom(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete { writeError(w, http.StatusMethodNotAllowed, "method not allowed"); return }
	username, ok := authenticate(r)
	if !ok { writeError(w, http.StatusUnauthorized, "invalid or missing session"); return }
	roomID := strings.TrimPrefix(r.URL.Path, "/api/rooms/")
	if roomID == "" || roomID == r.URL.Path { writeError(w, http.StatusNotFound, "not found"); return }
	if err := store.DeleteRoom(roomID, username); err != nil {
		status := http.StatusBadRequest
		switch err {
		case ErrNotOwner:
			status = http.StatusForbidden
		case ErrRoomNotFound:
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	// give connected members a reson before dropping them, rather than 
	// just yanking the connection and leaving them to guess why
	notifyRoomDeleted(roomID)
	writeJSON(w, http.StatusOK, map[string]string{ "status": "ok" })
}

func handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete { writeError(w, http.StatusMethodNotAllowed, "method not allowed"); return }
	username, ok := authenticate(r)
	if !ok { writeError(w, http.StatusUnauthorized, "invalid or missing session"); return }
	var req struct{ Password string }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil { writeError(w, http.StatusBadRequest, "invalid request body"); return }
	ownedRoomIDs, err := store.RoomIDsOwnedBy(username)
	if err != nil { writeError(w, http.StatusInternalServerError, "failed to load rooms"); return }
	if err := store.DeleteAccount(username, req.Password); err != nil {
		status := http.StatusBadRequest
		if err == ErrInvalidCreds { status = http.StatusUnauthorized }
		writeError(w, status, err.Error())
		return
	}
	for _, roomID := range ownedRoomIDs {
		notifyRoomDeleted(roomID)
	}
	writeJSON(w, http.StatusOK, map[string]string{ "status": "ok" })
}

// NOTE. handleInvite handles POST /api/rooms/{roomID}/invite
func handleInvite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost { writeError(w, http.StatusMethodNotAllowed, "method not allowed"); return }
	username, ok := authenticate(r)
	if !ok { writeError(w, http.StatusUnauthorized, "invalid or mission session"); return }
	path := strings.TrimPrefix(r.URL.Path, "/api/rooms/")
	roomID := strings.TrimSuffix(path, "/invite")
	if roomID == "" || roomID == path { writeError(w, http.StatusNotFound, "not found"); return }
	var req struct{ Username string }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil { writeError(w, http.StatusBadRequest, "invalid request body"); return }
	target := strings.TrimSpace(req.Username)
	if err := store.Invite(roomID, username, target); err != nil {
		status := http.StatusBadRequest
		switch err {
		case ErrNotOwner:
			status = http.StatusForbidden
		case ErrRoomNotFound, ErrUserNotFound:
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{ "status": "ok" })
}

// NOTE. handleRoomMembers handles GET /api/rooms/{roomID}/members: the full
// member list (connected or not) for any current member of the room, plus
// pending invites when the requester is the room's owner.
func handleRoomMembers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet { writeError(w, http.StatusMethodNotAllowed, "method not allowed"); return }
	username, ok := authenticate(r)
	if !ok { writeError(w, http.StatusUnauthorized, "invalid or missing session"); return }
	path := strings.TrimPrefix(r.URL.Path, "/api/rooms/")
	roomID := strings.TrimSuffix(path, "/members")
	if roomID == "" || roomID == path { writeError(w, http.StatusNotFound, "not found"); return }
	room, invites, err := store.RoomMembers(roomID, username)
	if err != nil {
		status := http.StatusBadRequest
		switch err {
		case ErrRoomNotFound:
			status = http.StatusNotFound
		case ErrNotMember:
			status = http.StatusForbidden
		}
		writeError(w, status, err.Error())
		return
	}
	pending := make([]string, 0, len(invites))
	for _, inv := range invites { pending = append(pending, inv.To) }
	writeJSON(w, http.StatusOK, RoomMembersResponse{Owner: room.Owner, Members: room.Members, PendingInvites: pending})
}

// NOTE. handleAcceptInvite handles POST /api/invites/{roomID}/accept
func handleAcceptInvite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost { writeError(w, http.StatusMethodNotAllowed, "method not allowed"); return }
	username, ok := authenticate(r)
	if !ok { writeError(w, http.StatusUnauthorized, "invalid or mission session"); return }
	path := strings.TrimPrefix(r.URL.Path, "/api/invites/")
	roomID := strings.TrimSuffix(path, "/accept")
	if roomID == "" || roomID == path { writeError(w, http.StatusNotFound, "not found"); return }
	if err := store.AcceptInvite(roomID, username); err != nil {
		status := http.StatusBadRequest
		if err == ErrNoSuchInvite || err == ErrRoomNotFound { status = http.StatusNotFound }
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{ "status": "ok" })
}