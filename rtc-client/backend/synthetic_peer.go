package backend

import (
	"fmt"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
)

// ----------------------------------------------------------------------
// synthetic peer: a room member that isn't a real client - it's a media
// source (e.g. /play) owned by this client, given its own identity by the
// signaling server so every other peer can see and volume-control it.
// ----------------------------------------------------------------------

var (
	syntheticMutex    sync.Mutex
	activeSyntheticID string                          	// "" if no synthetic owned right now
	syntheticTrack    *webrtc.TrackLocalStaticSample   	// shared outbound track, added to every syntheticPeers[*] connection
	syntheticLocalBuf *peerAudioBuffer                 	// this client's own Mixer entry for the synthetic (direct local feed, no RTP round-trip)

	syntheticPeersMutex sync.Mutex
	syntheticPeers      = make(map[string]*Peer) 		// keyed by REMOTE peer ID - mirrors `peers`, but for connections made "as" the synthetic

	syntheticWelcomeCh = make(chan syntheticWelcome, 1) // rendezvous: websocketReadLoop -> whoever is waiting on requestSpawn
)

// NOTE. requestSpawn asks the server to create a synthetic identity owned by us
// and blocks (bounded) for its reply.
func requestSpawn(name string) (id string, existingPeers []PeerInfo, err error) {
	sendSignalMessage(SignalMessage{Type: "spawn-synthetic", Data: mustJSON(map[string]string{"name": name})})
	select {
	case w := <-syntheticWelcomeCh:
		return w.id, w.peers, nil
	case <-time.After(5 * time.Second):
		return "", nil, fmt.Errorf("server didn't respond to spawn-synthetic")
	}
}

// NOTE. deliverSyntheticWelcome is called from websocketReadLoop's "synthetic-welcome" case.
func deliverSyntheticWelcome(id string, peers []PeerInfo) {
	select {
	case syntheticWelcomeCh <- syntheticWelcome{id: id, peers: peers}:
	default: // nobody waiting - drop rather than block the read loop
	}
}

// NOTE. drainRemoteTrack discards an inbound track without decoding or mixing it.
// A synthetic connection to remote peer X still receives X's mic track (X's
// own createPeerConnection always attaches it, regardless of which of our
// connections they're talking to) - we already hear X via our separate real
// connection to them, so mixing it again here would double their volume.
func drainRemoteTrack(track *webrtc.TrackRemote) {
	for {
		if _, _, err := track.ReadRTP(); err != nil { return }
	}
}

// NOTE. createSyntheticPeerConnection mirrors createPeerConnection in client.go,
// but for a connection made "as" a synthetic identity we own: it carries
// 'track' (the shared media track) instead of localAudioTrack, tags every
// outbound signaling message with As: syntheticID, cleans up syntheticPeers
// instead of peers on close, drains (never mixes) inbound remote audio, and
// must not hand its data channel to setupDataChannel - that function keys
// into the REAL peers map by remote peer ID and would silently overwrite
// that peer's live chat data channel, breaking chat with them.
func createSyntheticPeerConnection(remotePeerID string, syntheticID string, track *webrtc.TrackLocalStaticSample) *webrtc.PeerConnection {
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}},
	}
	peer, err := webrtc.NewPeerConnection(config)
	if err != nil { program.Send(ErrorMsg{Reason: err.Error()}); return nil }

	if track != nil {
		rtpSender, err := peer.AddTrack(track)
		if err != nil {
			program.Send(ErrorMsg{Reason: "add synthetic track error: " + err.Error()})
		} else {
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
		sendSignalMessage(SignalMessage{Type: "ice-candidate", To: remotePeerID, As: syntheticID, Data: mustJSON(candidate.ToJSON())})
	})

	peer.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		switch state {
		case webrtc.PeerConnectionStateFailed:
			offer, err := peer.CreateOffer(&webrtc.OfferOptions{ICERestart: true})
			if err != nil { program.Send(ErrorMsg{Reason: "synthetic ice restart offer error: " + err.Error()}); return }
			if err := peer.SetLocalDescription(offer); err != nil { program.Send(ErrorMsg{Reason: "synthetic ice restart set local description error: " + err.Error()}); return }
			sendSignalMessage(SignalMessage{Type: "offer", To: remotePeerID, As: syntheticID, Data: mustJSON(offer)})
		case webrtc.PeerConnectionStateClosed:
			syntheticPeersMutex.Lock()
			delete(syntheticPeers, remotePeerID)
			syntheticPeersMutex.Unlock()
			// no Mixer.removePeer here - the synthetic's Mixer entry is
			// the local direct-feed keyed by syntheticID, not by this
			// per-remote-peer connection.
		}
	})

	peer.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		go drainRemoteTrack(track)
	})

	// offering side (us, when spawning) creates a data channel; on the
	// answering side (someone calling our synthetic) just accept and close
	// it rather than wiring it into chat.
	peer.OnDataChannel(func(dataChannel *webrtc.DataChannel) {
		dataChannel.OnOpen(func() { dataChannel.Close() })
	})

	return peer
}

// NOTE. callSyntheticPeer offers to remotePeerID as syntheticID - the
// "whoever spawns calls everyone already present" half of the mesh rule,
// run by the owner on the synthetic's behalf once per already-present peer.
func callSyntheticPeer(syntheticID string, remotePeerID string) {
	syntheticMutex.Lock()
	track := syntheticTrack
	syntheticMutex.Unlock()

	peer := createSyntheticPeerConnection(remotePeerID, syntheticID, track)
	if peer == nil { return }
	syntheticPeersMutex.Lock()
	syntheticPeers[remotePeerID] = &Peer{peerConnection: peer}
	syntheticPeersMutex.Unlock()

	dataChannel, err := peer.CreateDataChannel("chat", nil)
	if err == nil { dataChannel.OnOpen(func() { dataChannel.Close() }) }

	offer, err := peer.CreateOffer(nil)
	if err != nil { program.Send(ErrorMsg{Reason: err.Error()}); return }
	if err := peer.SetLocalDescription(offer); err != nil { program.Send(ErrorMsg{Reason: err.Error()}); return }
	sendSignalMessage(SignalMessage{Type: "offer", To: remotePeerID, As: syntheticID, Data: mustJSON(offer)})
}

// NOTE. newSyntheticTrack mirrors the mic track's shape in audio.go: Opus,
// signaled as stereo=2 in the SDP capability (matching pion's
// default-registered codec) with the actual fmtp declaring mono.
func newSyntheticTrack() (*webrtc.TrackLocalStaticSample, error) {
	return webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeOpus,
			ClockRate:   sampleRate,
			Channels:    2,
			SDPFmtpLine: "minptime=10;useinbandfec=1;stereo=0",
		},
		"audio", "media",
	)
}

// NOTE. despawnSynthetic tears down every synthetic peer connection and the
// Mixer entry, and tells the server, if a synthetic is currently active.
func despawnSynthetic() {
	syntheticMutex.Lock()
	id := activeSyntheticID
	activeSyntheticID = ""
	syntheticTrack = nil
	syntheticLocalBuf = nil
	syntheticMutex.Unlock()
	if id == "" { return }

	syntheticPeersMutex.Lock()
	for rid, p := range syntheticPeers {
		p.peerConnection.Close()
		delete(syntheticPeers, rid)
	}
	syntheticPeersMutex.Unlock()

	Mixer.removePeer(id)
	sendSignalMessage(SignalMessage{Type: "despawn-synthetic", Data: mustJSON(map[string]string{"id": id})})
}
