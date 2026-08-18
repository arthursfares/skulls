package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)


var (
	ErrUserExists   = errors.New("username already taken")
	ErrInvalidCreds = errors.New("invalid username or password")
	ErrRoomNotFound = errors.New("room not found")
	ErrNotOwner     = errors.New("only the room owner can do that")
	ErrUserNotFound = errors.New("user not found")
	ErrNoSuchInvite = errors.New("no pending invite for this room")
	ErrNotMember    = errors.New("not a member of this room")
	ErrRoomLimitReached = errors.New("room limit reached; delete an existing room first")
	ErrRoomNameTaken = errors.New("you already have a room with that name")
)

// MaxRoomsPerUser caps how many rooms a single user may own at once.
const MaxRoomsPerUser = 10

// dbUser, dbRoom, dbRoomMember and dbInvite are the GORM models backing the
// store. They're distinct from RoomRecord/InviteRecord
// (the public API types) and from the in-memory Room type in
// types_definitions.go, which tracks connected websocket clients rather
// than persisted state.
type dbUser struct {
	Username     string `gorm:"primaryKey"`
	PasswordHash string `gorm:"not null"`
	CreatedAt    time.Time
}

func (dbUser) TableName() string { return "users" }

type dbRoom struct {
	ID        string `gorm:"primaryKey"`
	Name      string `gorm:"not null"`
	Owner     string `gorm:"not null"`
	CreatedAt time.Time
	// Members/Invites aren't populated by any query (members are fetched
	// separately via membersOf); they exist only so AutoMigrate emits the
	// room_id foreign key with ON DELETE CASCADE on room_members/invites.
	Members []dbRoomMember `gorm:"foreignKey:RoomID;references:ID;constraint:OnDelete:CASCADE"`
	Invites []dbInvite     `gorm:"foreignKey:RoomID;references:ID;constraint:OnDelete:CASCADE"`
}

func (dbRoom) TableName() string { return "rooms" }

type dbRoomMember struct {
	RoomID   string `gorm:"primaryKey;column:room_id"`
	Username string `gorm:"primaryKey"`
}

func (dbRoomMember) TableName() string { return "room_members" }

type dbInvite struct {
	RoomID    string `gorm:"primaryKey;column:room_id"`
	Username  string `gorm:"primaryKey"` // invitee
	FromUser  string `gorm:"not null;column:from_user"`
	CreatedAt time.Time
}

func (dbInvite) TableName() string { return "invites" }

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil { return "", err }
	return hex.EncodeToString(b), nil
}

func isUniqueConstraintErr(err error) bool {
	return err != nil &&strings.Contains(err.Error(), "UNIQUE constraint")
}


// NOTE. NewStore opens (creating if necessary) a SQLite database at path
// and ensures the schema exists. Sessions remain in-memory only.
func NewStore(path string) (*Store, error) {
	gdb, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil { return nil, err }
	sqlDB, err := gdb.DB()
	if err != nil { return nil, err }
	// SQLite only tolerates one writer at a time.
	// Keeping the pool to a single connection plus a busy_timeout
	// means concurrent writers queue up instead of failing with "database is locked"
	sqlDB.SetMaxOpenConns(1)
	for _, pragma := range []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA foreign_keys = ON;",
		"PRAGMA busy_timeout = 5000;",
	} {
		if err := gdb.Exec(pragma).Error; err != nil { sqlDB.Close(); return nil, err }
	}
	// create table schemas if not exist
	if err := gdb.AutoMigrate(&dbUser{}, &dbRoom{}, &dbRoomMember{}, &dbInvite{}); err != nil {
		sqlDB.Close(); return nil, err
	}
	// AutoMigrate can't express a case-insensitive composite unique
	// constraint, so add it directly (SQLite-specific COLLATE NOCASE).
	if err := gdb.Exec(
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_rooms_owner_name ON rooms(owner, name COLLATE NOCASE)`,
	).Error; err != nil { sqlDB.Close(); return nil, err }
	// return Store
	return &Store{
		db:			gdb,
		sessions: 	make(map[string]string),
	}, nil
}

// NOTE. Close releases the underlying database handle. Call this on shutdown.
func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil { return err }
	return sqlDB.Close()
}

func (s *Store) Register(username string, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil { return err }
	err = s.db.Create(&dbUser{
		Username:     username,
		PasswordHash: string(hash),
		CreatedAt:    time.Now(),
	}).Error
	if err != nil {
		if isUniqueConstraintErr(err) { return ErrUserExists }
		return err
	}
	return nil
}

func (s *Store) Login(username string, password string) (string, error) {
	var user dbUser
	err := s.db.Where("username = ?", username).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) { return "", ErrInvalidCreds }
		return "", err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return "", ErrInvalidCreds
	}
	token, err := newToken()
	if err != nil { return "", err }
	s.sessionsMu.Lock()
	s.sessions[token] = username
	s.sessionsMu.Unlock()
	return token, nil
}

func (s *Store) LookupSession(token string) (string, bool) {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	username, ok := s.sessions[token]
	return username, ok
}

// NOTE. DeleteAccount permanently removes a user, re-verifying their
// password first as a safety confirmation. It transactionally: deletes
// every room the own (cascading to that room's members/invites via the
// room_id FK), removes their membership in rooms owned by others, clears
// any pending invites addressed to them, then delets the user row.
// Sessions are in-memory, sso they're cleared separately afterward.
func (s *Store) DeleteAccount(username string, password string) error {
	var user dbUser
	err := s.db.Where("username = ?", username).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) { return ErrInvalidCreds }
		return err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil { return ErrInvalidCreds }
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("owner = ?", username).Delete(&dbRoom{}).Error; err != nil { return err }
		if err := tx.Where("username = ?", username).Delete(&dbRoomMember{}).Error; err != nil { return err }
		if err := tx.Where("username = ?", username).Delete(&dbInvite{}).Error; err != nil { return err }
		if err := tx.Where("username = ?", username).Delete(&dbUser{}).Error; err != nil { return err }
		return nil
	})
	if err != nil { return err }
	s.sessionsMu.Lock()
	for token, u := range s.sessions {
		if u == username { delete(s.sessions, token) }
	}
	s.sessionsMu.Unlock()
	return nil
}

// NOTE. CountRoomsOwnedBy returns how many rooms username currently owns.
func (s *Store) CountRoomsOwnedBy(username string) (int, error) {
	var count int64
	err := s.db.Model(&dbRoom{}).Where("owner = ?", username).Count(&count).Error
	if err != nil { return 0, err }
	return int(count), nil
}

func (s *Store) CreateRoom(name string, owner string) (RoomRecord, error) {
	var room RoomRecord
	err := s.db.Transaction(func(tx *gorm.DB) error {
		// Re-check the count inside the transaction so two concurrent creates
		// from the same user can't both slip in under the limit.
		var count int64
		if err := tx.Model(&dbRoom{}).Where("owner = ?", owner).Count(&count).Error; err != nil {
			return err
		}
		if count >= MaxRoomsPerUser { return ErrRoomLimitReached }
		r := dbRoom{
			ID:        uuid.NewString(),
			Name:      name,
			Owner:     owner,
			CreatedAt: time.Now(),
		}
		if err := tx.Create(&r).Error; err != nil {
			if isUniqueConstraintErr(err) { return ErrRoomNameTaken }
			return err
		}
		if err := tx.Create(&dbRoomMember{RoomID: r.ID, Username: owner}).Error; err != nil {
			return err
		}
		room = RoomRecord{
			ID:        r.ID,
			Name:      r.Name,
			Owner:     r.Owner,
			Members:   []string{owner},
			CreatedAt: r.CreatedAt,
		}
		return nil
	})
	if err != nil { return RoomRecord{}, err }
	return room, nil
}

// NOTE. DeleteRoom removes a room and all associated memberships/invites,
// but only if requester is the current owner. Casscading FK deletes handle
// room_members and invites
func (s *Store) DeleteRoom(roomID string, requester string) error {
	var room dbRoom
	err := s.db.Where("id = ?", roomID).First(&room).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) { return ErrRoomNotFound }
		return err
	}
	if room.Owner != requester { return ErrNotOwner }
	return s.db.Where("id = ?", roomID).Delete(&dbRoom{}).Error
}

// NOTE. RoomsFor returns every room the given user is a member of
// (owner or accepted invitee).
func (s *Store) RoomsFor(username string) ([]RoomRecord, error) {
	var rooms []dbRoom
	err := s.db.
		Joins("JOIN room_members m ON m.room_id = rooms.id").
		Where("m.username = ?", username).
		Order("rooms.created_at DESC").
		Find(&rooms).Error
	if err != nil { return nil, err }
	// Fill in members for each room.
	// (Separate queries rather than a join so a room with N memebers
	// does not get duplicated N times above)
	out := make([]RoomRecord, len(rooms))
	for i, r := range rooms {
		members, err := s.membersOf(r.ID)
		if err != nil { return nil, err }
		out[i] = RoomRecord{
			ID:        r.ID,
			Name:      r.Name,
			Owner:     r.Owner,
			Members:   members,
			CreatedAt: r.CreatedAt,
		}
	}
	return out, nil
}

func (s *Store) membersOf(roomID string) ([]string, error) {
	var members []string
	err := s.db.Model(&dbRoomMember{}).Where("room_id = ?", roomID).Pluck("username", &members).Error
	return members, err
}

func (s *Store) InvitesFor(username string) ([]InviteRecord, error) {
	var rows []struct {
		RoomID    string
		RoomName  string
		FromUser  string
		Username  string
		CreatedAt time.Time
	}
	err := s.db.Table("invites AS i").
		Select("i.room_id, r.name AS room_name, i.from_user, i.username, i.created_at").
		Joins("JOIN rooms r ON r.id = i.room_id").
		Where("i.username = ?", username).
		Scan(&rows).Error
	if err != nil { return nil, err }
	out := make([]InviteRecord, len(rows))
	for i, row := range rows {
		out[i] = InviteRecord{
			RoomID:    row.RoomID,
			RoomName:  row.RoomName,
			From:      row.FromUser,
			To:        row.Username,
			CreatedAt: row.CreatedAt,
		}
	}
	return out, nil
}

// NOTE. RoomMembers returns a room's full member list (owner + everyone who
// accepted an invite, connected or not), gated on requester being a member.
// If requester is the room's owner, invites is also populated with everyone
// who has a pending invite to the room; otherwise it's nil.
func (s *Store) RoomMembers(roomID string, requester string) (room RoomRecord, invites []InviteRecord, err error) {
	var r dbRoom
	err = s.db.Where("id = ?", roomID).First(&r).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) { return RoomRecord{}, nil, ErrRoomNotFound }
		return RoomRecord{}, nil, err
	}
	isMember, err := s.IsMember(roomID, requester)
	if err != nil { return RoomRecord{}, nil, err }
	if !isMember { return RoomRecord{}, nil, ErrNotMember }
	members, err := s.membersOf(roomID)
	if err != nil { return RoomRecord{}, nil, err }
	room = RoomRecord{ID: r.ID, Name: r.Name, Owner: r.Owner, Members: members, CreatedAt: r.CreatedAt}
	if r.Owner != requester { return room, nil, nil }
	invites, err = s.PendingInvitesFor(roomID)
	if err != nil { return RoomRecord{}, nil, err }
	return room, invites, nil
}

// NOTE. PendingInvitesFor returns every outstanding invite for a room,
// regardless of who it's addressed to. Owner-only callers use this to see
// who's been invited but hasn't accepted yet.
func (s *Store) PendingInvitesFor(roomID string) ([]InviteRecord, error) {
	var rows []struct {
		RoomID    string
		RoomName  string
		FromUser  string
		Username  string
		CreatedAt time.Time
	}
	err := s.db.Table("invites AS i").
		Select("i.room_id, r.name AS room_name, i.from_user, i.username, i.created_at").
		Joins("JOIN rooms r ON r.id = i.room_id").
		Where("i.room_id = ?", roomID).
		Scan(&rows).Error
	if err != nil { return nil, err }
	out := make([]InviteRecord, len(rows))
	for i, row := range rows {
		out[i] = InviteRecord{ RoomID: row.RoomID, RoomName: row.RoomName, From: row.FromUser, To: row.Username, CreatedAt: row.CreatedAt }
	}
	return out, nil
}

func (s *Store) IsMember(roomID string, username string) (bool, error) {
	var count int64
	err := s.db.Model(&dbRoomMember{}).
		Where("room_id = ? AND username = ?", roomID, username).
		Count(&count).Error
	if err != nil { return false, err }
	return count > 0, nil
}

// NOTE. RoomIDsOwnedBy returns the IDs of rooms owned by username. Callers that
// need to notify connected members before a cascading delete should grab
// this at first, since the rows won't exist anymore afterward.
func (s *Store) RoomIDsOwnedBy(username string) ([]string, error) {
	var ids []string
	err := s.db.Model(&dbRoom{}).Where("owner = ?", username).Pluck("id", &ids).Error
	return ids, err
}

// NOTE. Invite records a pending invitation from an owner to another user.
// It's a no-op (not an error) if the target is already a member or already
// invited, so retrying is harmless.
func (s *Store) Invite(roomID string, from string, to string) error {
	// check if it is room owner sending the invite
	var room dbRoom
	err := s.db.Where("id = ?", roomID).First(&room).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) { return ErrRoomNotFound }
		return err
	}
	if room.Owner != from { return ErrNotOwner }
	// check if the invited user exists
	var userCount int64
	if err := s.db.Model(&dbUser{}).Where("username = ?", to).Count(&userCount).Error; err != nil {
		return err
	}
	if userCount == 0 { return ErrUserNotFound }
	// check if the invited user is not already a member
	isMember, err := s.IsMember(roomID, to)
	if err != nil { return err }
	if isMember { return nil }
	// add invite
	err = s.db.Create(&dbInvite{RoomID: roomID, Username: to, FromUser: from, CreatedAt: time.Now()}).Error
	if err != nil {
		if isUniqueConstraintErr(err) { return nil } // already invited
		return err
	}
	return nil
}

func (s *Store) AcceptInvite(roomID string, username string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		// delete invite entry
		res := tx.Where("room_id = ? AND username = ?", roomID, username).Delete(&dbInvite{})
		if res.Error != nil { return res.Error }
		if res.RowsAffected == 0 { return ErrNoSuchInvite }
		// check if room exists
		var roomCount int64
		if err := tx.Model(&dbRoom{}).Where("id = ?", roomID).Count(&roomCount).Error; err != nil {
			return err
		}
		if roomCount == 0 { return ErrRoomNotFound }
		// add invited user to room members
		return tx.Create(&dbRoomMember{RoomID: roomID, Username: username}).Error
	})
}
