package backend

import (
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"
	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
)

var (
	program *tea.Program 						// set by main() via SetProgram once the tea.Program is constructed

	websocketConn *websocket.Conn
	myID          string
	peersMutex    sync.Mutex
	peers         = make(map[string]*Peer)

	currentUsername string 						// set by SetCurrentUsername at login, used to scope saved peer volumes
	currentRoomID   string 						// set in ConnectCmd, used to scope saved peer volumes
	peerUsernamesMu sync.Mutex
	peerUsernames   = make(map[string]string) 	// peerID -> username

	websocketSendQueue chan SignalMessage  		// created fresh per connection

	muted 		atomic.Bool
	leavingRoom atomic.Bool
)

// NOTE. SetProgram wires up the tea.Program the networking/audio side sends
// messages to. Must be called once, before ConnectCmd or any audio callback
// can fire.
func SetProgram(p *tea.Program) { program = p }

func SetCurrentUsername(username string) { currentUsername = username }

// NOTE. connectCmd is a tea.Cmd: bubbletea runs it in its own goroutine and
// feeds whatever tea.Msg it returns back into Update().
func ConnectCmd(token string, roomID string) tea.Cmd {
	return func() tea.Msg {
		currentRoomID = roomID
		if err := initAudio(); err != nil { return ErrorMsg{Reason: "audio init error: " + err.Error()} }
		// rawURL := url.URL{Scheme: "ws", Host: "localhost:8080", Path: "/ws", RawQuery: "token=" + url.QueryEscape(token) + "&room=" + url.QueryEscape(roomID)}
		// NOTE. changed scheme to 'wss' and no port for remote signaling server
		host := os.Getenv("SIGNALING_SERVER_URL")
		host = strings.TrimPrefix(host, "https://")
		rawURL := url.URL{Scheme: "wss", Host: host, Path: "/ws", RawQuery: "token=" + url.QueryEscape(token) + "&room=" + url.QueryEscape(roomID)}
		conn, _, err := websocket.DefaultDialer.Dial(rawURL.String(), nil)
		if err != nil { StopAudio(); return ErrorMsg{Reason: err.Error()} }
		websocketConn = conn
		websocketSendQueue = make(chan SignalMessage, 32)

		go websocketWriteLoop()
		go websocketReadLoop()
		return ConnectedMsg{}
	}
}

func ToggleMuteCmd() tea.Cmd {
	return func() tea.Msg {
		newState := !muted.Load()
		muted.Store(newState)
		return MuteChangedMsg{Muted: newState}
	}
}

// NOTE. teardownCall closes every peer connection, the websocket, and stops audio.
func teardownCall() {
	// stop the media pipeline and close synthetic connections first
	stopPipeline()
	syntheticPeersMutex.Lock()
	for id, p := range syntheticPeers { p.peerConnection.Close(); delete(syntheticPeers, id) }
	syntheticPeersMutex.Unlock()
	syntheticMutex.Lock()
	activeSyntheticID, syntheticTrack, syntheticLocalBuf = "", nil, nil
	syntheticMutex.Unlock()

	peersMutex.Lock()
	for id, p := range peers { p.peerConnection.Close(); delete(peers, id) }
	peersMutex.Unlock()
	if websocketConn != nil { websocketConn.Close(); websocketConn = nil }
	if websocketSendQueue != nil { close(websocketSendQueue); websocketSendQueue = nil }
	StopAudio()
	captureAccum = nil
	Mixer = newAudioMixer()
	myID = ""
	currentRoomID = ""
	peerUsernamesMu.Lock()
	peerUsernames = make(map[string]string)
	peerUsernamesMu.Unlock()
}

func LeaveRoomCmd() tea.Cmd {
	return func() tea.Msg {
		leavingRoom.Store(true)
		defer leavingRoom.Store(false)
		teardownCall()
		return LeftRoomMsg{}
	}
}

// NOTE. reads the JSON messages in the send queue channel and writes them
//	to the websocket connection
func websocketWriteLoop() {
	for msg := range websocketSendQueue {
		err := websocketConn.WriteJSON(msg)
		if err != nil {
			program.Send(ErrorMsg{Reason: "ws write error: " + err.Error()})
			return
		}
	}
}

func sendSignalMessage(msg SignalMessage) {
	if websocketSendQueue == nil { return }
	defer func() { recover() }()  // channel may close concurrently during teardown
	websocketSendQueue <- msg
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// ----------------------------------------------------------------------
// chat: webrtc data channel, one per peer, carried alongside the audio track
// ----------------------------------------------------------------------

// NOTE. setupDataChannel wires a data channel's callbacks and stashes it on
// the Peer so broadcastToAllPeers can find it later. Used both when we
// create the channel ourselves (offering side) and when the remote side
// creates it and hands it to us via OnDataChannel (answering side).
func setupDataChannel(dataChannel *webrtc.DataChannel, peerID string) {
	peersMutex.Lock()
	if peer, ok := peers[peerID]; ok { peer.dataChannel = dataChannel }
	peersMutex.Unlock()

	dataChannel.OnOpen(func() {
		program.Send(LogMsg{Text: "chat channel open with " + peerID})
	})
	dataChannel.OnMessage(func(msg webrtc.DataChannelMessage) {
		var envelope dataChannelMsg
		if err := json.Unmarshal(msg.Data, &envelope); err != nil { return }
		program.Send(ChatMsg{From: peerID, Text: envelope.Text, Private: envelope.Kind == "private"})
	})
}

// NOTE. broadcastToAllPeers fans a chat message out over every open data
// channel. Peers whose channel hasn't opened yet (or has closed) are
// silently skipped rather than erroring - the message just won't reach them.
func BroadcastToAllPeers(text string) {
	peersMutex.Lock()
	defer peersMutex.Unlock()
	for _, peer := range peers {
		if peer.dataChannel == nil { continue }
		if peer.dataChannel.ReadyState() != webrtc.DataChannelStateOpen { continue }
		peer.dataChannel.Send(mustJSON(dataChannelMsg{Kind: "chat", Text: text}))
	}
}

// NOTE. SendToPeer sends text on only the given peer's data channel, so nobody
// else in the room receives it. Returns false if the peer isn't known or
// its channel isn't open yet, so the caller can report the failure.
func SendToPeer(peerID string, text string) bool {
	peersMutex.Lock()
	defer peersMutex.Unlock()
	peer, ok := peers[peerID]
	if !ok || peer.dataChannel == nil { return false }
	if peer.dataChannel.ReadyState() != webrtc.DataChannelStateOpen { return false }
	peer.dataChannel.Send(mustJSON(dataChannelMsg{Kind: "private", Text: text}))
	return true
}

// NOTE. RequestRoll asks the signaling server to roll dice on our behalf and
// broadcast the result to the room, so no single peer can fake the outcome.
func RequestRoll(notation string) {
	sendSignalMessage(SignalMessage{Type: "roll-request", Data: mustJSON(map[string]string{"notation": notation})})
}

// ----------------------------------------------------------------------
// WebRTC signaling / peer connection plumbing
// ----------------------------------------------------------------------

func createPeerConnection(peerID string) *webrtc.PeerConnection {
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}},
	}
	peer, err := webrtc.NewPeerConnection(config)
	if err != nil { program.Send(ErrorMsg{Reason: err.Error()}); return nil }

	if localAudioTrack != nil {
		rtpSender, err := peer.AddTrack(localAudioTrack)
		if err != nil {
			program.Send(ErrorMsg{Reason: "add track error: " + err.Error()})
		} else {
			// drain RTCP so interceptors (NACK, etc.) keep working
			go func() {
				rtcpBuf := make([]byte, 1500)
				for {
					if _, _, rtcpErr := rtpSender.Read(rtcpBuf); rtcpErr != nil { return }
				}
			}()
		}
	}

	peer.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil { return }
		sendSignalMessage(SignalMessage{Type: "ice-candidate", To: peerID, Data: mustJSON(candidate.ToJSON())})
	})

	peer.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		switch state {
		case webrtc.PeerConnectionStateFailed:
			offer, err := peer.CreateOffer(&webrtc.OfferOptions{ICERestart: true})
			if err != nil { program.Send(ErrorMsg{Reason: "ice restart offer error: " + err.Error()}); return }
			if err := peer.SetLocalDescription(offer); err != nil { program.Send(ErrorMsg{Reason: "ice restart set local description error: " + err.Error()}); return }
			sendSignalMessage(SignalMessage{Type: "offer", To: peerID, Data: mustJSON(offer)})
		case webrtc.PeerConnectionStateClosed:
			peersMutex.Lock()
			delete(peers, peerID)
			peersMutex.Unlock()
			Mixer.removePeer(peerID)
		}
	})

	peer.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		go readRemoteAudio(peerID, track)
	})

	// the answering side gets the chat channel handed to it here; the
	// offering side creates it explicitly in callPeer instead.
	peer.OnDataChannel(func(dataChannel *webrtc.DataChannel) {
		setupDataChannel(dataChannel, peerID)
	})

	return peer
}


// NOTE. establish a call (offer) to another peer
func callPeer(peerID string) {
	peerConnection := createPeerConnection(peerID)
	if peerConnection == nil { return }
	peersMutex.Lock()
	peers[peerID] = &Peer{peerConnection: peerConnection}
	peersMutex.Unlock()

	// offering side creates the chat data channel up front; the remote side
	// receives it via OnDataChannel once negotiation completes.
	dataChannel, err := peerConnection.CreateDataChannel("chat", nil)
	if err != nil {
		program.Send(ErrorMsg{Reason: "create data channel error: " + err.Error()})
	} else {
		setupDataChannel(dataChannel, peerID)
	}

	offer, err := peerConnection.CreateOffer(nil)
	if err != nil { program.Send(ErrorMsg{Reason: err.Error()}); return }
	if err := peerConnection.SetLocalDescription(offer); err != nil { program.Send(ErrorMsg{Reason: err.Error()}); return }
	sendSignalMessage(SignalMessage{Type: "offer", To: peerID, Data: mustJSON(offer)})
}

// NOTE. controls the behaviour based on the msg returned from the server
func websocketReadLoop() {
	disconnectedReason := ""  // set by a server notice before the conn closes

	for {
		var msg SignalMessage
		err := websocketConn.ReadJSON(&msg)
		if err != nil {
			if !leavingRoom.Load() {
				teardownCall()
				reason := disconnectedReason
				if reason == "" { reason = "disconnected from server: " + err.Error() }
				program.Send(DisconnectedMsg{ Reason: reason })
			}
			return
		}

		switch msg.Type {
		case "welcome":
			var payload struct {
				ID    string     `json:"id"`
				Name  string     `json:"name"`
				Peers []PeerInfo `json:"peers"`
			}
			json.Unmarshal(msg.Data, &payload)
			myID = payload.ID
			peerUsernamesMu.Lock()
			for _, p := range payload.Peers { peerUsernames[p.ID] = p.Name }
			peerUsernamesMu.Unlock()
			for _, p := range payload.Peers {
				go callPeer(p.ID)
			}
			program.Send(WelcomeMsg{ID: myID, Peers: payload.Peers})

		case "offer":
			peerID := msg.From
			// synthetic ----------------
			syntheticMutex.Lock()
			synthID, track := activeSyntheticID, syntheticTrack
			syntheticMutex.Unlock()
			if synthID != "" && msg.To == synthID {
				// this offer is addressed to a synthetic we own -
				// route to the synthetic connection map, not the real one.
				peerConnection := createSyntheticPeerConnection(peerID, synthID, track)
				if peerConnection == nil { continue }
				syntheticPeersMutex.Lock()
				syntheticPeers[peerID] = &Peer{peerConnection: peerConnection}
				syntheticPeersMutex.Unlock()

				var offer webrtc.SessionDescription
				json.Unmarshal(msg.Data, &offer)
				if err := peerConnection.SetRemoteDescription(offer); err != nil { program.Send(ErrorMsg{Reason: err.Error()}); continue }
				answer, err := peerConnection.CreateAnswer(nil)
				if err != nil { program.Send(ErrorMsg{Reason: err.Error()}); continue }
				if err := peerConnection.SetLocalDescription(answer); err != nil { program.Send(ErrorMsg{Reason: err.Error()}); continue }
				sendSignalMessage(SignalMessage{Type: "answer", To: peerID, As: synthID, Data: mustJSON(answer)})
				continue
			}
			// --------------------------

			peerConnection := createPeerConnection(peerID)
			if peerConnection == nil {
				continue
			}
			peersMutex.Lock()
			peers[peerID] = &Peer{peerConnection: peerConnection}
			peersMutex.Unlock()

			var offer webrtc.SessionDescription
			json.Unmarshal(msg.Data, &offer)
			if err := peerConnection.SetRemoteDescription(offer); err != nil { program.Send(ErrorMsg{Reason: err.Error()}); continue }
			answer, err := peerConnection.CreateAnswer(nil)
			if err != nil { program.Send(ErrorMsg{Reason: err.Error()}); continue }
			if err := peerConnection.SetLocalDescription(answer); err != nil { program.Send(ErrorMsg{Reason: err.Error()}); continue }
			sendSignalMessage(SignalMessage{Type: "answer", To: peerID, Data: mustJSON(answer)})

		case "answer":
			// synthetic ----------------
			syntheticMutex.Lock()
			synthID := activeSyntheticID
			syntheticMutex.Unlock()
			if synthID != "" && msg.To == synthID {
				syntheticPeersMutex.Lock()
				peer, ok := syntheticPeers[msg.From]
				syntheticPeersMutex.Unlock()
				if !ok { program.Send(ErrorMsg{Reason: "synthetic answer from unknown peer: " + msg.From}); continue }
				var answer webrtc.SessionDescription
				json.Unmarshal(msg.Data, &answer)
				if err := peer.peerConnection.SetRemoteDescription(answer); err != nil { program.Send(ErrorMsg{Reason: "set remote description error: " + err.Error()}) }
				continue
			}
			// --------------------------

			peersMutex.Lock()
			peer, ok := peers[msg.From]
			peersMutex.Unlock()
			if !ok { program.Send(ErrorMsg{Reason: "answer from unknown peer: " + msg.From}); continue }
			var answer webrtc.SessionDescription
			json.Unmarshal(msg.Data, &answer)
			if err := peer.peerConnection.SetRemoteDescription(answer); err != nil { program.Send(ErrorMsg{Reason: "set remote description error: " + err.Error()}) }

		case "ice-candidate":
			syntheticMutex.Lock()
			synthID := activeSyntheticID
			syntheticMutex.Unlock()
			if synthID != "" && msg.To == synthID {
				syntheticPeersMutex.Lock()
				peer, ok := syntheticPeers[msg.From]
				syntheticPeersMutex.Unlock()
				if !ok { continue }
				var candidate webrtc.ICECandidateInit
				json.Unmarshal(msg.Data, &candidate)
				if err := peer.peerConnection.AddICECandidate(candidate); err != nil { program.Send(ErrorMsg{Reason: "add ice candidate error: " + err.Error()}) }
				continue
			}

			peersMutex.Lock()
			peer, ok := peers[msg.From]
			peersMutex.Unlock()
			if !ok { continue }
			var candidate webrtc.ICECandidateInit
			json.Unmarshal(msg.Data, &candidate)
			if err := peer.peerConnection.AddICECandidate(candidate); err != nil { program.Send(ErrorMsg{Reason: "add ice candidate error: " + err.Error()}) }

		case "synthetic-welcome":
			var payload struct {
				ID    string     `json:"id"`
				Peers []PeerInfo `json:"peers"`
			}
			json.Unmarshal(msg.Data, &payload)
			deliverSyntheticWelcome(payload.ID, payload.Peers)

		case "peer-joined":
			var info PeerInfo
			json.Unmarshal(msg.Data, &info)
			peerUsernamesMu.Lock()
			peerUsernames[info.ID] = info.Name
			peerUsernamesMu.Unlock()
			program.Send(PeerJoinedMsg{ID: info.ID, Name: info.Name, Synthetic: info.Synthetic})

		case "peer-left":
			peersMutex.Lock()
			delete(peers, msg.From)
			peersMutex.Unlock()
			peerUsernamesMu.Lock()
			delete(peerUsernames, msg.From)
			peerUsernamesMu.Unlock()
			Mixer.removePeer(msg.From)
			program.Send(PeerLeftMsg{ID: msg.From})

		case "roll-result":
			var payload struct {
				Roller   string `json:"roller"`
				Notation string `json:"notation"`
				Results  []int  `json:"results"`
				Total    int    `json:"total"`
			}
			json.Unmarshal(msg.Data, &payload)
			program.Send(RollResultMsg{Roller: payload.Roller, Notation: payload.Notation, Results: payload.Results, Total: payload.Total})

		case "room-deleted":
			disconnectedReason = "the room owner deleted this room"

		case "error":
			program.Send(ErrorMsg{Reason: string(msg.Data)})
		}
	}
}
