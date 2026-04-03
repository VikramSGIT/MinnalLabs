package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

type PhaseState struct {
	Products []ProductInfo          `json:"products"`
	Users    map[string]*UserRecord `json:"users"`
}

type ProductInfo struct {
	ProductID int    `json:"product_id"`
	Name      string `json:"name"`
}

type UserRecord struct {
	Slot              int                    `json:"slot"`
	UserID            int                    `json:"user_id,omitempty"`
	Username          string                 `json:"username,omitempty"`
	Password          string                 `json:"password,omitempty"`
	SessionToken      string                 `json:"session_token,omitempty"`
	OAuthAccessToken  string                 `json:"oauth_access_token,omitempty"`
	OAuthRefreshToken string                 `json:"oauth_refresh_token,omitempty"`
	Deleted           bool                   `json:"deleted,omitempty"`
	DeleteMode        string                 `json:"delete_mode,omitempty"`
	Homes             map[string]*HomeRecord `json:"homes"`
}

type HomeRecord struct {
	HomeSlot        int                      `json:"home_slot"`
	HomeID          int                      `json:"home_id,omitempty"`
	Name            string                   `json:"name,omitempty"`
	DeleteRequested bool                     `json:"delete_requested,omitempty"`
	Devices         map[string]*DeviceRecord `json:"devices"`
}

type DeviceRecord struct {
	DeviceSlot  int    `json:"device_slot"`
	DeviceID    int    `json:"device_id,omitempty"`
	ProductID   int    `json:"product_id,omitempty"`
	ProductName string `json:"product_name,omitempty"`
	CompoundID  string `json:"compound_id,omitempty"`
	Name        string `json:"name,omitempty"`
	Deleted     bool   `json:"deleted,omitempty"`
}

type UserSnapshot struct {
	Slot              int
	UserID            int
	Username          string
	Password          string
	SessionToken      string
	OAuthAccessToken  string
	OAuthRefreshToken string
	Deleted           bool
}

type HomeSnapshot struct {
	Slot            int
	HomeSlot        int
	HomeID          int
	Name            string
	DeleteRequested bool
}

type DeviceSnapshot struct {
	Slot        int
	HomeSlot    int
	DeviceSlot  int
	DeviceID    int
	ProductID   int
	ProductName string
	CompoundID  string
	Name        string
	Deleted     bool
}

type PhaseStateStore struct {
	path  string
	mu    sync.RWMutex
	state PhaseState
	dirty bool
}

func loadPhaseStateStore(path string) (*PhaseStateStore, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read phase state: %w", err)
	}
	var state PhaseState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("parse phase state: %w", err)
	}
	if state.Users == nil {
		state.Users = map[string]*UserRecord{}
	}
	return &PhaseStateStore{
		path:  path,
		state: state,
	}, nil
}

func (s *PhaseStateStore) StartAutoFlush(stop <-chan struct{}, every time.Duration, logf func(string, ...any)) {
	if every <= 0 {
		return
	}
	ticker := time.NewTicker(every)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if err := s.Flush(); err != nil && logf != nil {
					logf("phase state flush failed: %v", err)
				}
			}
		}
	}()
}

func (s *PhaseStateStore) Products() []ProductInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	products := make([]ProductInfo, len(s.state.Products))
	copy(products, s.state.Products)
	return products
}

func (s *PhaseStateStore) User(slot int) (UserSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.state.Users[strconv.Itoa(slot)]
	if !ok || record == nil {
		return UserSnapshot{}, false
	}
	return UserSnapshot{
		Slot:              slot,
		UserID:            record.UserID,
		Username:          record.Username,
		Password:          record.Password,
		SessionToken:      record.SessionToken,
		OAuthAccessToken:  record.OAuthAccessToken,
		OAuthRefreshToken: record.OAuthRefreshToken,
		Deleted:           record.Deleted,
	}, true
}

func (s *PhaseStateStore) Home(slot, homeSlot int) (HomeSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	user, ok := s.state.Users[strconv.Itoa(slot)]
	if !ok || user == nil || user.Homes == nil {
		return HomeSnapshot{}, false
	}
	home, ok := user.Homes[strconv.Itoa(homeSlot)]
	if !ok || home == nil {
		return HomeSnapshot{}, false
	}
	return HomeSnapshot{
		Slot:            slot,
		HomeSlot:        homeSlot,
		HomeID:          home.HomeID,
		Name:            home.Name,
		DeleteRequested: home.DeleteRequested,
	}, true
}

func (s *PhaseStateStore) Device(slot, homeSlot, deviceSlot int) (DeviceSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	user, ok := s.state.Users[strconv.Itoa(slot)]
	if !ok || user == nil || user.Homes == nil {
		return DeviceSnapshot{}, false
	}
	home, ok := user.Homes[strconv.Itoa(homeSlot)]
	if !ok || home == nil || home.Devices == nil {
		return DeviceSnapshot{}, false
	}
	device, ok := home.Devices[strconv.Itoa(deviceSlot)]
	if !ok || device == nil {
		return DeviceSnapshot{}, false
	}
	return DeviceSnapshot{
		Slot:        slot,
		HomeSlot:    homeSlot,
		DeviceSlot:  deviceSlot,
		DeviceID:    device.DeviceID,
		ProductID:   device.ProductID,
		ProductName: device.ProductName,
		CompoundID:  device.CompoundID,
		Name:        device.Name,
		Deleted:     device.Deleted,
	}, true
}

func (s *PhaseStateStore) UpsertUser(slot, userID int, username, password, sessionToken, oauthAccessToken, oauthRefreshToken string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.ensureUser(slot)
	record.UserID = userID
	record.Username = username
	record.Password = password
	record.SessionToken = sessionToken
	record.OAuthAccessToken = oauthAccessToken
	record.OAuthRefreshToken = oauthRefreshToken
	s.dirty = true
}

func (s *PhaseStateStore) UpdateSession(slot int, sessionToken string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.ensureUser(slot)
	record.SessionToken = sessionToken
	s.dirty = true
}

func (s *PhaseStateStore) UpdateOAuth(slot int, accessToken, refreshToken string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.ensureUser(slot)
	record.OAuthAccessToken = accessToken
	record.OAuthRefreshToken = refreshToken
	s.dirty = true
}

func (s *PhaseStateStore) UpsertHome(slot, homeSlot, homeID int, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.ensureHome(slot, homeSlot)
	record.HomeID = homeID
	record.Name = name
	s.dirty = true
}

func (s *PhaseStateStore) MarkHomeDeleteRequested(slot, homeSlot int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.ensureHome(slot, homeSlot)
	record.DeleteRequested = true
	s.dirty = true
}

func (s *PhaseStateStore) UpsertDevice(slot, homeSlot, deviceSlot, deviceID, productID int, productName, compoundID, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.ensureDevice(slot, homeSlot, deviceSlot)
	record.DeviceID = deviceID
	record.ProductID = productID
	record.ProductName = productName
	record.CompoundID = compoundID
	record.Name = name
	s.dirty = true
}

func (s *PhaseStateStore) MarkDeviceDeleted(slot, homeSlot, deviceSlot int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.ensureDevice(slot, homeSlot, deviceSlot)
	record.Deleted = true
	s.dirty = true
}

func (s *PhaseStateStore) MarkUserDeleted(slot int, mode string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.ensureUser(slot)
	record.Deleted = true
	record.DeleteMode = mode
	s.dirty = true
}

func (s *PhaseStateStore) ensureUser(slot int) *UserRecord {
	if s.state.Users == nil {
		s.state.Users = map[string]*UserRecord{}
	}
	key := strconv.Itoa(slot)
	record, ok := s.state.Users[key]
	if !ok || record == nil {
		record = &UserRecord{
			Slot:  slot,
			Homes: map[string]*HomeRecord{},
		}
		s.state.Users[key] = record
	}
	if record.Homes == nil {
		record.Homes = map[string]*HomeRecord{}
	}
	return record
}

func (s *PhaseStateStore) ensureHome(slot, homeSlot int) *HomeRecord {
	user := s.ensureUser(slot)
	key := strconv.Itoa(homeSlot)
	record, ok := user.Homes[key]
	if !ok || record == nil {
		record = &HomeRecord{
			HomeSlot: homeSlot,
			Devices:  map[string]*DeviceRecord{},
		}
		user.Homes[key] = record
	}
	if record.Devices == nil {
		record.Devices = map[string]*DeviceRecord{}
	}
	return record
}

func (s *PhaseStateStore) ensureDevice(slot, homeSlot, deviceSlot int) *DeviceRecord {
	home := s.ensureHome(slot, homeSlot)
	key := strconv.Itoa(deviceSlot)
	record, ok := home.Devices[key]
	if !ok || record == nil {
		record = &DeviceRecord{DeviceSlot: deviceSlot}
		home.Devices[key] = record
	}
	return record
}

func (s *PhaseStateStore) Flush() error {
	s.mu.Lock()
	if !s.dirty {
		s.mu.Unlock()
		return nil
	}
	payload, err := json.MarshalIndent(s.state, "", "  ")
	if err == nil {
		s.dirty = false
	}
	s.mu.Unlock()
	if err != nil {
		return fmt.Errorf("marshal phase state: %w", err)
	}

	tmpPath := filepath.Join(filepath.Dir(s.path), "."+filepath.Base(s.path)+".tmp")
	if err := os.WriteFile(tmpPath, payload, 0o644); err != nil {
		s.markDirty()
		return fmt.Errorf("write phase state temp file: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		s.markDirty()
		return fmt.Errorf("swap phase state file: %w", err)
	}
	return nil
}

func (s *PhaseStateStore) markDirty() {
	s.mu.Lock()
	s.dirty = true
	s.mu.Unlock()
}
