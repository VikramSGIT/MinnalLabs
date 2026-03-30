package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	gooauth2 "github.com/go-oauth2/oauth2/v4"
	oauthmodels "github.com/go-oauth2/oauth2/v4/models"
	localmodels "github.com/iot-backend/internal/models"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

const (
	oauthCodeKeyPrefix    = "oauth:code:"
	oauthAccessKeyPrefix  = "oauth:access:"
	oauthRefreshKeyPrefix = "oauth:refresh:"
)

type lookupKind string

const (
	lookupCode    lookupKind = "code"
	lookupAccess  lookupKind = "access"
	lookupRefresh lookupKind = "refresh"
)

type PostgresTokenStore struct {
	rdb *redis.Client
	gdb *gorm.DB
}

func NewPostgresTokenStore(rdb *redis.Client, gdb *gorm.DB) (gooauth2.TokenStore, error) {
	if gdb == nil {
		return nil, errors.New("oauth token store requires database")
	}
	return &PostgresTokenStore{
		rdb: rdb,
		gdb: gdb,
	}, nil
}

func (ts *PostgresTokenStore) Create(ctx context.Context, info gooauth2.TokenInfo) error {
	token := cloneTokenInfo(info)
	payload, err := json.Marshal(token)
	if err != nil {
		return err
	}

	record := localmodels.OAuthToken{
		ClientID:  token.GetClientID(),
		UserID:    token.GetUserID(),
		Code:      token.GetCode(),
		Access:    token.GetAccess(),
		Refresh:   token.GetRefresh(),
		Data:      string(payload),
		ExpiresIn: primaryExpirySeconds(token),
		ExpiresAt: tokenRecordExpiresAt(token),
		CreatedAt: tokenCreatedAt(token),
	}

	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}

	db := ts.gdb.WithContext(ctx)
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := deleteMatchingTokenRows(tx, token); err != nil {
			return err
		}
		return tx.Create(&record).Error
	}); err != nil {
		return err
	}

	ts.cacheToken(ctx, token, payload)
	return nil
}

func (ts *PostgresTokenStore) RemoveByCode(ctx context.Context, code string) error {
	return ts.removeByLookup(ctx, lookupCode, code)
}

func (ts *PostgresTokenStore) RemoveByAccess(ctx context.Context, access string) error {
	return ts.removeByLookup(ctx, lookupAccess, access)
}

func (ts *PostgresTokenStore) RemoveByRefresh(ctx context.Context, refresh string) error {
	return ts.removeByLookup(ctx, lookupRefresh, refresh)
}

func (ts *PostgresTokenStore) GetByCode(ctx context.Context, code string) (gooauth2.TokenInfo, error) {
	token, _, err := ts.getByLookup(ctx, lookupCode, code)
	if err != nil || token == nil {
		return nil, err
	}
	return token, nil
}

func (ts *PostgresTokenStore) GetByAccess(ctx context.Context, access string) (gooauth2.TokenInfo, error) {
	token, _, err := ts.getByLookup(ctx, lookupAccess, access)
	if err != nil || token == nil {
		return nil, err
	}
	return token, nil
}

func (ts *PostgresTokenStore) GetByRefresh(ctx context.Context, refresh string) (gooauth2.TokenInfo, error) {
	token, _, err := ts.getByLookup(ctx, lookupRefresh, refresh)
	if err != nil || token == nil {
		return nil, err
	}
	return token, nil
}

func (ts *PostgresTokenStore) removeByLookup(ctx context.Context, kind lookupKind, value string) error {
	if value == "" {
		return nil
	}

	token, record, err := ts.getByLookup(ctx, kind, value)
	if err != nil {
		return err
	}
	if record != nil {
		if err := ts.gdb.WithContext(ctx).Delete(&localmodels.OAuthToken{}, record.ID).Error; err != nil {
			return err
		}
	}

	if token != nil {
		ts.deleteCacheKeys(ctx, token)
		return nil
	}

	ts.deleteLookupCacheKey(ctx, kind, value)
	return nil
}

func (ts *PostgresTokenStore) getByLookup(ctx context.Context, kind lookupKind, value string) (*oauthmodels.Token, *localmodels.OAuthToken, error) {
	if value == "" {
		return nil, nil, nil
	}

	if token, err := ts.getCachedToken(ctx, kind, value); err != nil {
		return nil, nil, err
	} else if token != nil {
		return token, nil, nil
	}

	record, err := ts.findTokenRecord(ctx, kind, value)
	if err != nil {
		return nil, nil, err
	}
	if record == nil {
		return nil, nil, nil
	}

	token, err := tokenFromJSON(record.Data)
	if err != nil {
		return nil, nil, err
	}
	if token == nil {
		return nil, record, nil
	}

	if tokenExpiredForLookup(token, kind, time.Now().UTC()) {
		if err := ts.gdb.WithContext(ctx).Delete(&localmodels.OAuthToken{}, record.ID).Error; err != nil {
			return nil, nil, err
		}
		ts.deleteCacheKeys(ctx, token)
		return nil, nil, nil
	}

	if !tokenMatchesLookup(token, kind, value) {
		return nil, nil, nil
	}

	ts.cacheToken(ctx, token, []byte(record.Data))
	return token, record, nil
}

func (ts *PostgresTokenStore) getCachedToken(ctx context.Context, kind lookupKind, value string) (*oauthmodels.Token, error) {
	if ts.rdb == nil {
		return nil, nil
	}

	data, err := ts.rdb.Get(ctx, lookupCacheKey(kind, value)).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return tokenFromJSON(data)
}

func (ts *PostgresTokenStore) findTokenRecord(ctx context.Context, kind lookupKind, value string) (*localmodels.OAuthToken, error) {
	db := ts.gdb.WithContext(ctx)
	var record localmodels.OAuthToken

	query := db.Order("id DESC")
	switch kind {
	case lookupCode:
		query = query.Where("code = ?", value)
	case lookupAccess:
		query = query.Where("access = ?", value)
	case lookupRefresh:
		query = query.Where("refresh = ?", value)
	default:
		return nil, nil
	}

	if err := query.First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

func deleteMatchingTokenRows(tx *gorm.DB, token *oauthmodels.Token) error {
	query := tx.Where("1 = 0")
	argsAdded := false

	if code := token.GetCode(); code != "" {
		query = query.Or("code = ?", code)
		argsAdded = true
	}
	if access := token.GetAccess(); access != "" {
		query = query.Or("access = ?", access)
		argsAdded = true
	}
	if refresh := token.GetRefresh(); refresh != "" {
		query = query.Or("refresh = ?", refresh)
		argsAdded = true
	}

	if !argsAdded {
		return nil
	}
	return query.Delete(&localmodels.OAuthToken{}).Error
}

func (ts *PostgresTokenStore) cacheToken(ctx context.Context, token *oauthmodels.Token, payload []byte) {
	if ts.rdb == nil {
		return
	}

	ts.cacheLookup(ctx, lookupCode, token.GetCode(), payload, tokenCodeTTL(token))
	ts.cacheLookup(ctx, lookupAccess, token.GetAccess(), payload, tokenAccessTTL(token))
	ts.cacheLookup(ctx, lookupRefresh, token.GetRefresh(), payload, tokenRefreshTTL(token))
}

func (ts *PostgresTokenStore) cacheLookup(ctx context.Context, kind lookupKind, value string, payload []byte, ttl time.Duration) {
	if value == "" || ttl < 0 {
		return
	}

	key := lookupCacheKey(kind, value)
	if ttl == 0 {
		_ = ts.rdb.Set(ctx, key, string(payload), 0).Err()
		return
	}
	_ = ts.rdb.Set(ctx, key, string(payload), ttl).Err()
}

func (ts *PostgresTokenStore) deleteCacheKeys(ctx context.Context, token *oauthmodels.Token) {
	if ts.rdb == nil {
		return
	}

	keys := make([]string, 0, 3)
	if code := token.GetCode(); code != "" {
		keys = append(keys, lookupCacheKey(lookupCode, code))
	}
	if access := token.GetAccess(); access != "" {
		keys = append(keys, lookupCacheKey(lookupAccess, access))
	}
	if refresh := token.GetRefresh(); refresh != "" {
		keys = append(keys, lookupCacheKey(lookupRefresh, refresh))
	}
	if len(keys) == 0 {
		return
	}
	_ = ts.rdb.Del(ctx, keys...).Err()
}

func (ts *PostgresTokenStore) deleteLookupCacheKey(ctx context.Context, kind lookupKind, value string) {
	if ts.rdb == nil || value == "" {
		return
	}
	_ = ts.rdb.Del(ctx, lookupCacheKey(kind, value)).Err()
}

func lookupCacheKey(kind lookupKind, value string) string {
	switch kind {
	case lookupCode:
		return oauthCodeKeyPrefix + value
	case lookupRefresh:
		return oauthRefreshKeyPrefix + value
	default:
		return oauthAccessKeyPrefix + value
	}
}

func tokenFromJSON(data string) (*oauthmodels.Token, error) {
	if data == "" {
		return nil, nil
	}

	var token oauthmodels.Token
	if err := json.Unmarshal([]byte(data), &token); err != nil {
		return nil, err
	}
	return &token, nil
}

func cloneTokenInfo(info gooauth2.TokenInfo) *oauthmodels.Token {
	token := oauthmodels.NewToken()
	token.SetClientID(info.GetClientID())
	token.SetUserID(info.GetUserID())
	token.SetRedirectURI(info.GetRedirectURI())
	token.SetScope(info.GetScope())
	token.SetCode(info.GetCode())
	token.SetCodeCreateAt(info.GetCodeCreateAt())
	token.SetCodeExpiresIn(info.GetCodeExpiresIn())
	token.SetCodeChallenge(info.GetCodeChallenge())
	token.SetCodeChallengeMethod(info.GetCodeChallengeMethod())
	token.SetAccess(info.GetAccess())
	token.SetAccessCreateAt(info.GetAccessCreateAt())
	token.SetAccessExpiresIn(info.GetAccessExpiresIn())
	token.SetRefresh(info.GetRefresh())
	token.SetRefreshCreateAt(info.GetRefreshCreateAt())
	token.SetRefreshExpiresIn(info.GetRefreshExpiresIn())
	if extendable, ok := info.(gooauth2.ExtendableTokenInfo); ok {
		token.SetExtension(extendable.GetExtension())
	}
	return token
}

func tokenCreatedAt(token *oauthmodels.Token) time.Time {
	switch {
	case token.GetAccess() != "" && !token.GetAccessCreateAt().IsZero():
		return token.GetAccessCreateAt()
	case token.GetCode() != "" && !token.GetCodeCreateAt().IsZero():
		return token.GetCodeCreateAt()
	case token.GetRefresh() != "" && !token.GetRefreshCreateAt().IsZero():
		return token.GetRefreshCreateAt()
	default:
		return time.Time{}
	}
}

func primaryExpirySeconds(token *oauthmodels.Token) int64 {
	switch {
	case token.GetRefresh() != "":
		return int64(token.GetRefreshExpiresIn() / time.Second)
	case token.GetCode() != "":
		return int64(token.GetCodeExpiresIn() / time.Second)
	default:
		return int64(token.GetAccessExpiresIn() / time.Second)
	}
}

func tokenRecordExpiresAt(token *oauthmodels.Token) *time.Time {
	switch {
	case token.GetRefresh() != "" && token.GetRefreshExpiresIn() > 0:
		expiresAt := token.GetRefreshCreateAt().Add(token.GetRefreshExpiresIn())
		return &expiresAt
	case token.GetCode() != "" && token.GetCodeExpiresIn() > 0:
		expiresAt := token.GetCodeCreateAt().Add(token.GetCodeExpiresIn())
		return &expiresAt
	case token.GetAccess() != "" && token.GetAccessExpiresIn() > 0:
		expiresAt := token.GetAccessCreateAt().Add(token.GetAccessExpiresIn())
		return &expiresAt
	default:
		return nil
	}
}

func tokenMatchesLookup(token *oauthmodels.Token, kind lookupKind, value string) bool {
	switch kind {
	case lookupCode:
		return token.GetCode() == value
	case lookupRefresh:
		return token.GetRefresh() == value
	default:
		return token.GetAccess() == value
	}
}

func tokenExpiredForLookup(token *oauthmodels.Token, kind lookupKind, now time.Time) bool {
	switch kind {
	case lookupCode:
		return token.GetCode() == "" || expiryReached(token.GetCodeCreateAt(), token.GetCodeExpiresIn(), now)
	case lookupRefresh:
		return token.GetRefresh() == "" || expiryReached(token.GetRefreshCreateAt(), token.GetRefreshExpiresIn(), now)
	default:
		if token.GetAccess() == "" || expiryReached(token.GetAccessCreateAt(), token.GetAccessExpiresIn(), now) {
			return true
		}
		if token.GetRefresh() != "" && token.GetRefreshExpiresIn() != 0 &&
			expiryReached(token.GetRefreshCreateAt(), token.GetRefreshExpiresIn(), now) {
			return true
		}
		return false
	}
}

func tokenCodeTTL(token *oauthmodels.Token) time.Duration {
	if token.GetCode() == "" {
		return -1
	}
	return remainingTTL(token.GetCodeCreateAt(), token.GetCodeExpiresIn())
}

func tokenAccessTTL(token *oauthmodels.Token) time.Duration {
	if token.GetAccess() == "" {
		return -1
	}

	ttl := remainingTTL(token.GetAccessCreateAt(), token.GetAccessExpiresIn())
	if token.GetRefresh() == "" || token.GetRefreshExpiresIn() == 0 {
		return ttl
	}

	refreshTTL := remainingTTL(token.GetRefreshCreateAt(), token.GetRefreshExpiresIn())
	switch {
	case refreshTTL < 0:
		return refreshTTL
	case ttl == 0:
		return refreshTTL
	case refreshTTL == 0:
		return ttl
	case refreshTTL < ttl:
		return refreshTTL
	default:
		return ttl
	}
}

func tokenRefreshTTL(token *oauthmodels.Token) time.Duration {
	if token.GetRefresh() == "" {
		return -1
	}
	return remainingTTL(token.GetRefreshCreateAt(), token.GetRefreshExpiresIn())
}

func remainingTTL(createdAt time.Time, expiresIn time.Duration) time.Duration {
	if expiresIn == 0 {
		return 0
	}
	if createdAt.IsZero() {
		return -1
	}

	ttl := time.Until(createdAt.Add(expiresIn))
	if ttl <= 0 {
		return -1
	}
	return ttl
}

func expiryReached(createdAt time.Time, expiresIn time.Duration, now time.Time) bool {
	if expiresIn == 0 {
		return false
	}
	if createdAt.IsZero() {
		return true
	}
	return !createdAt.Add(expiresIn).After(now)
}
