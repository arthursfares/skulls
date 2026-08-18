package backend

import (
	"encoding/json"
	"os"
	"path/filepath"
)


// NOTE. PeerVolumesDir returns the fixed directory saved peer volumes live under
// ($HOME/.config/rtc-client/peer_volumes).
func PeerVolumesDir() string {
	dir, err := configBaseDir()
	if err != nil { return "." }
	return filepath.Join(dir, "peer_volumes")
}

const peerVolumesFilename = "peer_volumes.json"

// NOTE. peerVolumesFilePath scopes saved volumes to accountUsername
func peerVolumesFilePath(accountUsername string) string {
	return filepath.Join(PeerVolumesDir(), sanitizeFilename(accountUsername), peerVolumesFilename)
}

func LoadPeerVolumes(accountUsername string) PeerVolumes {
	data, err := os.ReadFile(peerVolumesFilePath(accountUsername))
	if err != nil { return PeerVolumes{} }
	var v PeerVolumes
	if err := json.Unmarshal(data, &v); err != nil { return PeerVolumes{} }
	if v == nil { v = PeerVolumes{} }
	return v
}

func SavePeerVolumes(accountUsername string, v PeerVolumes) error {
	path := peerVolumesFilePath(accountUsername)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil { return err }
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil { return err }
	return os.WriteFile(path, data, 0o600)
}

// NOTE. Get returns the saved gain for peerUsername in roomID, and whether one
// was ever saved.
func (v PeerVolumes) Get(roomID, peerUsername string) (float64, bool) {
	room, ok := v[roomID]
	if !ok { return 0, false }
	g, ok := room[peerUsername]
	return g, ok
}

// NOTE. Set records peerUsername's gain in roomID.
func (v PeerVolumes) Set(roomID, peerUsername string, gain float64) {
	if v[roomID] == nil { v[roomID] = map[string]float64{} }
	v[roomID][peerUsername] = gain
}

func SavePeerVolume(accountUsername, roomID, peerUsername string, gain float64) {
	if accountUsername == "" || roomID == "" || peerUsername == "" { return }
	v := LoadPeerVolumes(accountUsername)
	v.Set(roomID, peerUsername, gain)
	SavePeerVolumes(accountUsername, v)
}

// NOTE. applySavedPeerGain restores peerID's persisted gain onto buf, right after
// its Mixer entry is created in readRemoteAudio.
func applySavedPeerGain(peerID string, buf *peerAudioBuffer) {
	peerUsernamesMu.Lock()
	peerUsername := peerUsernames[peerID]
	peerUsernamesMu.Unlock()
	if peerUsername == "" || currentRoomID == "" || currentUsername == "" { return }

	if gain, ok := LoadPeerVolumes(currentUsername).Get(currentRoomID, peerUsername); ok {
		buf.setGain(gain)
	}
}
