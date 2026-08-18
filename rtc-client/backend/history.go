package backend

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"golang.org/x/crypto/scrypt"
)


var historyKey []byte // derived from the account password at login, session-only

// NOTE. SetHistoryKey sets the key used to encrypt/decrypt local chat history for
// the current session. Never persisted to disk.
func SetHistoryKey(key []byte) { historyKey = key }

// NOTE. DeriveHistoryKey derives a per-account local-storage key from the login
// password. Deterministic per (username, password) pair so history saved on
// a previous session can be decrypted again after logging back in - but the
// key itself never touches disk.
func DeriveHistoryKey(username, password string) []byte {
	salt := []byte("rtc-client-history:" + username)
	key, err := scrypt.Key([]byte(password), salt, 32768, 8, 1, 32)
	if err != nil { return nil }
	return key
}

func configBaseDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil { return "", err }
	dir := filepath.Join(home, ".config", "rtc-client")
	if err := os.MkdirAll(dir, 0o700); err != nil { return "", err }
	return dir, nil
}

// NOTE. HistoryDir returns the fixed directory chat history is saved under
// ($HOME/.config/rtc-client/history).
func HistoryDir() string {
	dir, err := configBaseDir()
	if err != nil { return "." }
	return filepath.Join(dir, "history")
}

func settingsFilePath() (string, error) {
	dir, err := configBaseDir()
	if err != nil { return "", err }
	return filepath.Join(dir, "settings.json"), nil
}

// NOTE. LoadHistorySettings reads the persisted dashboard settings, falling back
// to defaults (history saving off) if the file is missing or unreadable.
func LoadHistorySettings() HistorySettings {
	defaults := HistorySettings{Enabled: false}
	path, err := settingsFilePath()
	if err != nil { return defaults }
	data, err := os.ReadFile(path)
	if err != nil { return defaults }
	var s HistorySettings
	if err := json.Unmarshal(data, &s); err != nil { return defaults }
	return s
}

func SaveHistorySettings(s HistorySettings) error {
	path, err := settingsFilePath()
	if err != nil { return err }
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil { return err }
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil { return err }
	return os.WriteFile(path, data, 0o600)
}

func sanitizeFilename(s string) string {
	unsafeFilenameChars := regexp.MustCompile(`[^a-zA-Z0-9_-]+`)
	sanitized := unsafeFilenameChars.ReplaceAllString(s, "_")
	if sanitized == "" { return "_" }
	return sanitized
}


const historyFilename = "chat_history.enc"

func historyFilePath(username string) string {
	return filepath.Join(HistoryDir(), sanitizeFilename(username), historyFilename)
}

func newGCM() (cipher.AEAD, error) {
	if len(historyKey) == 0 { return nil, errors.New("no history key set") }
	block, err := aes.NewCipher(historyKey)
	if err != nil { return nil, err }
	return cipher.NewGCM(block)
}

// NOTE. AppendHistoryEntry encrypts entry and appends it as one line to the local
// history file for username. A no-op if history saving is disabled or no
// key has been set (e.g. not logged in this session).
func AppendHistoryEntry(settings HistorySettings, username string, entry HistoryEntry) error {
	if !settings.Enabled || len(historyKey) == 0 { return nil }
	gcm, err := newGCM()
	if err != nil { return err }
	plaintext, err := json.Marshal(entry)
	if err != nil { return err }
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil { return err }
	sealed := gcm.Seal(nonce, nonce, plaintext, nil)
	line := base64.StdEncoding.EncodeToString(sealed) + "\n"

	path := historyFilePath(username)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil { return err }
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil { return err }
	defer f.Close()
	_, err = f.WriteString(line)
	return err
}

// NOTE. LoadHistory reads and decrypts username's local history file and returns
// only the entries tagged with roomID. Lines that fail to decode/decrypt
// are skipped rather than aborting the whole load. Returns
// (nil, nil) if history saving is disabled, no key has been set, or no file
// exists yet.
func LoadHistory(settings HistorySettings, username, roomID string) ([]HistoryEntry, error) {
	if !settings.Enabled || len(historyKey) == 0 { return nil, nil }
	gcm, err := newGCM()
	if err != nil { return nil, err }
	path := historyFilePath(username)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) { return nil, nil }
		return nil, err
	}
	defer f.Close()

	var entries []HistoryEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		sealed, err := base64.StdEncoding.DecodeString(scanner.Text())
		if err != nil || len(sealed) < gcm.NonceSize() { continue }
		nonce, ciphertext := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
		plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
		if err != nil { continue }
		var entry HistoryEntry
		if err := json.Unmarshal(plaintext, &entry); err != nil { continue }
		if entry.RoomID != roomID { continue }
		entries = append(entries, entry)
	}
	return entries, scanner.Err()
}
