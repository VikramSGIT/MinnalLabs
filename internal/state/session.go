package state

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"time"
)

type SessionInfo struct {
	UserID     uint      `json:"user_id"`
	Username   string    `json:"username"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
}

func sessionKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return "session:" + hex.EncodeToString(sum[:])
}

func CreateSession(userID uint, username string) (string, SessionInfo, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", SessionInfo{}, err
	}

	now := time.Now().UTC()
	info := SessionInfo{
		UserID:     userID,
		Username:   username,
		CreatedAt:  now,
		LastSeenAt: now,
	}

	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	if err := cacheJSONTTL(sessionKey(token), info, sessionTTL); err != nil {
		return "", SessionInfo{}, err
	}

	return token, info, nil
}

func GetSession(token string) (SessionInfo, bool) {
	if token == "" {
		return SessionInfo{}, false
	}

	var info SessionInfo
	return info, getJSON(sessionKey(token), &info)
}

func TouchSession(token string) (SessionInfo, bool) {
	info, ok := GetSession(token)
	if !ok {
		return SessionInfo{}, false
	}

	info.LastSeenAt = time.Now().UTC()
	if err := cacheJSONTTL(sessionKey(token), info, sessionTTL); err != nil {
		return SessionInfo{}, false
	}

	return info, true
}

func DeleteSession(token string) {
	if token == "" {
		return
	}

	rdb.Del(ctx, sessionKey(token))
}

func cacheJSONTTL(key string, v interface{}, ttl time.Duration) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}

	return rdb.Set(ctx, key, string(data), ttl).Err()
}
