package backend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
)


const dashboardRefreshInterval = 5 * time.Second

var httpClient = &http.Client{Timeout: 10 * time.Second}


func apiRequest(method string, path string, token string, body any, out any) error {
	var reqBody *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil { return err }
		reqBody = bytes.NewReader(b)
	} else {
		reqBody = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, os.Getenv("SIGNALING_SERVER_URL") + path, reqBody)
	if err != nil { return err }
	req.Header.Set("Content-Type", "application/json")
	if token != "" { req.Header.Set("Authorization", "Bearer "+token) }
	resp, err := httpClient.Do(req)
	if err != nil { return err }
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		var errBody struct { Error string `json:"error"` }
		json.NewDecoder(resp.Body).Decode(&errBody)
		if errBody.Error != "" { return fmt.Errorf("%s", errBody.Error) }
		return fmt.Errorf("request failed: %s", resp.Status)
	}
	if out != nil { return json.NewDecoder(resp.Body).Decode(out) }
	return nil
}

func apiPost(path string, token string, body any, out any) error {
	return apiRequest(http.MethodPost, path, token, body, out)
}

func apiGet(path string, token string, out any) error {
	return apiRequest(http.MethodGet, path, token, nil, out)
}

func apiDelete(path string, token string, out any) error {
	return apiRequest(http.MethodDelete, path, token, nil, out)
}


func RegisterCmd(username string, password string) tea.Cmd {
	return func() tea.Msg {
		body := map[string]string{"username": username, "password": password}
		err := apiPost("/api/register", "", body, nil)
		if err != nil { return ErrorMsg{ Reason: err.Error() } }
		return RegisteredMsg{}
	}
}

func LoginCmd(username string, password string) tea.Cmd {
	return func() tea.Msg {
		var resp struct {
			Token		string		`json:"token"`
			Username	string		`json:"username"`
		}
		body := map[string]string{"username": username, "password": password}
		err := apiPost("/api/login", "", body, &resp)
		if err != nil { return ErrorMsg{ Reason: err.Error() } }
		return LoggedInMsg{ Token: resp.Token, Username: resp.Username }
	}
}

func FetchRoomsCmd(token string) tea.Cmd {
	return func() tea.Msg {
		var resp roomsListResponse
		err := apiGet("/api/rooms", token, &resp)
		if err != nil { return ErrorMsg{ Reason: err.Error() } }
		return RoomsMsg{ Rooms: resp.Rooms, Invites: resp.Invites }
	}
}

// NOTE. RefreshTickCmd schedules the next automatic dashboard refresh. It fires
// once and must be re-sent on every RefreshTickMsg to keep ticking.
func RefreshTickCmd() tea.Cmd {
	return tea.Tick(dashboardRefreshInterval, func(time.Time) tea.Msg {
		return RefreshTickMsg{}
	})
}

func CreateRoomCmd(token string, name string) tea.Cmd {
	return func() tea.Msg {
		var room RoomSummary
		body := map[string]string{ "name": name }
		err := apiPost("/api/rooms", token, body, &room)
		if err != nil { return ErrorMsg{ Reason: err.Error() } }
		var resp roomsListResponse
		err = apiGet("/api/rooms", token, &resp)
		if err != nil { return ErrorMsg{ Reason: err.Error() } }
		return RoomsMsg{ Rooms: resp.Rooms, Invites: resp.Invites }
	}
}

func DeleteRoomCmd(token string, roomID string) tea.Cmd {
	return func() tea.Msg {
		err := apiDelete("/api/rooms/"+roomID, token, nil)
		if err != nil { return ErrorMsg{ Reason: err.Error() } }
		// refresh aftert mutate
		var resp roomsListResponse
		err = apiGet("/api/rooms", token, &resp)
		if err != nil { return ErrorMsg{ Reason: err.Error() } }
		return RoomsMsg{ Rooms: resp.Rooms, Invites: resp.Invites }
	}
}

func DeleteAccountCmd(token string, password string) tea.Cmd {
	return func() tea.Msg {
		body := map[string]string{ "password": password }
		err := apiRequest(http.MethodDelete, "/api/account", token, body, nil)
		if err != nil { return ErrorMsg{ Reason: err.Error() } }
		return AccountDeletedMsg{}
	}
}

func InviteUserCmd(token string, roomID string, username string) tea.Cmd {
	return func() tea.Msg {
		body := map[string]string{ "username": username }
		err := apiPost("/api/rooms/"+roomID+"/invite", token, body, nil)
		if err != nil { return ErrorMsg{ Reason: err.Error() } }
		return InviteSentMsg{}
	}
}

func RoomMembersCmd(token string, roomID string) tea.Cmd {
	return func() tea.Msg {
		var resp RoomMembersResponse
		err := apiGet("/api/rooms/"+roomID+"/members", token, &resp)
		if err != nil { return ErrorMsg{ Reason: err.Error() } }
		return RoomMembersMsg{ Owner: resp.Owner, Members: resp.Members, PendingInvites: resp.PendingInvites }
	}
}

func AcceptInviteCmd(token string, roomID string) tea.Cmd {
	return func() tea.Msg {
		err := apiPost("/api/invites/"+roomID+"/accept", token, nil, nil)
		if err != nil { return ErrorMsg{ Reason: err.Error() } }
		var resp roomsListResponse
		err = apiGet("/api/rooms", token, &resp)
		if err != nil { return ErrorMsg{ Reason: err.Error() } }
		return RoomsMsg{ Rooms: resp.Rooms, Invites: resp.Invites }
	}
}
