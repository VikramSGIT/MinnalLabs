package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	BaseURL                      string
	SessionCookieName            string
	OAuthClientID                string
	OAuthClientSecret            string
	OAuthRedirectURI             string
	RunID                        string
	DevicePublicKey              string
	ProductPrefix                string
	ExpectedProductCount         int
	UserCount                    int
	HomesPerUser                 int
	DevicesPerHome               int
	DeleteDevicesPerUser         int
	DeleteHomesPerUser           int
	DeleteHomeSlot               int
	FulfillmentRequestsPerDevice int
	FulfillmentDeviceLimit       int
	DeleteUsersSelfCount         int
	DeleteUsersSelfStartIndex    int
	DeleteUsersAdminCount        int
	DeleteUsersAdminStartIndex   int
	AsyncHomeReadyTimeout        time.Duration
	AsyncHomeReadyPollInterval   time.Duration
	AsyncHomeEarlyReadyCheck     time.Duration
	HTTPTimeout                  time.Duration
	AdminUsername                string
	AdminPassword                string
	PhaseStatePath               string
	RawPath                      string
	SummaryPath                  string
	Phase                        PhaseConfig
}

type PhaseConfig struct {
	Name          string
	TotalItems    int
	Workers       int
	StartRPS      float64
	PeakRPS       float64
	RampUp        time.Duration
	Hold          time.Duration
	RampDown      time.Duration
	MaxDuration   time.Duration
	ScenarioLabel string
}

func loadConfig() (Config, error) {
	cfg := Config{
		BaseURL:                      mustEnv("BASE_URL"),
		SessionCookieName:            firstEnv("SESSION_COOKIE_NAME", "user_session"),
		OAuthClientID:                firstEnv("OAUTH_CLIENT_ID", "google-client"),
		OAuthClientSecret:            mustEnv("OAUTH_CLIENT_SECRET"),
		OAuthRedirectURI:             firstEnv("OAUTH_REDIRECT_URI", "http://127.0.0.1/oauth/callback"),
		RunID:                        mustEnv("STRESS_RUN_ID"),
		DevicePublicKey:              firstEnv("STRESS_DEVICE_PUBLIC_KEY", firstEnv("K6_DEVICE_PUBLIC_KEY", "")),
		ProductPrefix:                firstEnv("STRESS_PRODUCT_PREFIX", firstEnv("K6_PRODUCT_PREFIX", "stress-product-")),
		ExpectedProductCount:         intEnv(20, "STRESS_EXPECTED_PRODUCT_COUNT", "K6_EXPECTED_PRODUCT_COUNT"),
		UserCount:                    intEnv(200, "STRESS_USER_COUNT", "K6_USER_COUNT"),
		HomesPerUser:                 intEnv(2, "STRESS_HOMES_PER_USER", "K6_HOMES_PER_USER"),
		DevicesPerHome:               intEnv(20, "STRESS_DEVICES_PER_HOME", "K6_DEVICES_PER_HOME"),
		DeleteDevicesPerUser:         intEnv(10, "STRESS_DELETE_DEVICES_PER_USER", "K6_DELETE_DEVICES_PER_USER"),
		DeleteHomesPerUser:           intEnv(1, "STRESS_DELETE_HOMES_PER_USER", "K6_DELETE_HOMES_PER_USER"),
		DeleteHomeSlot:               intEnv(0, "STRESS_DELETE_HOME_SLOT", "K6_DELETE_HOME_SLOT"),
		FulfillmentRequestsPerDevice: intEnv(6, "STRESS_FULFILLMENT_REQUESTS_PER_DEVICE", "K6_FULFILLMENT_REQUESTS_PER_DEVICE"),
		FulfillmentDeviceLimit:       intEnv(0, "STRESS_FULFILLMENT_DEVICE_LIMIT", "K6_FULFILLMENT_DEVICE_LIMIT"),
		DeleteUsersSelfCount:         intEnv(100, "STRESS_DELETE_USERS_SELF_COUNT", "K6_DELETE_USERS_SELF_COUNT"),
		DeleteUsersSelfStartIndex:    intEnv(0, "STRESS_DELETE_USERS_SELF_START_INDEX", "K6_DELETE_USERS_SELF_START_INDEX"),
		DeleteUsersAdminCount:        intEnv(100, "STRESS_DELETE_USERS_ADMIN_COUNT", "K6_DELETE_USERS_ADMIN_COUNT"),
		DeleteUsersAdminStartIndex:   intEnv(100, "STRESS_DELETE_USERS_ADMIN_START_INDEX", "K6_DELETE_USERS_ADMIN_START_INDEX"),
		AsyncHomeReadyTimeout:        millisEnv(90000, "ASYNC_HOME_READY_TIMEOUT_MS"),
		AsyncHomeReadyPollInterval:   millisEnv(2000, "ASYNC_HOME_READY_POLL_MS"),
		AsyncHomeEarlyReadyCheck:     millisEnv(9000, "ASYNC_HOME_EARLY_READY_CHECK_MS"),
		HTTPTimeout:                  durationEnv("30s", "STRESS_HTTP_TIMEOUT"),
		AdminUsername:                os.Getenv("STRESS_ADMIN_USERNAME"),
		AdminPassword:                os.Getenv("STRESS_ADMIN_PASSWORD"),
		PhaseStatePath:               mustEnv("STRESS_PHASE_STATE_PATH"),
		RawPath:                      mustEnv("STRESS_PHASE_RAW_PATH"),
		SummaryPath:                  mustEnv("STRESS_PHASE_SUMMARY_PATH"),
	}

	cfg.Phase = PhaseConfig{
		Name:          mustEnv("STRESS_ACTIVE_PHASE"),
		TotalItems:    intEnv(0, "STRESS_PHASE_TOTAL_ITEMS"),
		Workers:       intEnv(1, "STRESS_PHASE_WORKERS"),
		StartRPS:      floatEnv(0, "STRESS_PHASE_START_RPS"),
		PeakRPS:       floatEnv(0, "STRESS_PHASE_PEAK_RPS"),
		RampUp:        secondsEnv("STRESS_PHASE_RAMP_UP_SECONDS"),
		Hold:          secondsEnv("STRESS_PHASE_HOLD_SECONDS"),
		RampDown:      secondsEnv("STRESS_PHASE_RAMP_DOWN_SECONDS"),
		MaxDuration:   durationEnv("1m", "STRESS_PHASE_MAX_DURATION"),
		ScenarioLabel: strings.TrimSpace(os.Getenv("STRESS_ACTIVE_PHASE")),
	}

	if cfg.DeleteUsersAdminStartIndex == 100 && firstEnv("STRESS_DELETE_USERS_ADMIN_START_INDEX", "K6_DELETE_USERS_ADMIN_START_INDEX") == "" {
		cfg.DeleteUsersAdminStartIndex = cfg.DeleteUsersSelfStartIndex + cfg.DeleteUsersSelfCount
	}
	if cfg.DevicePublicKey == "" {
		return cfg, fmt.Errorf("STRESS_DEVICE_PUBLIC_KEY or K6_DEVICE_PUBLIC_KEY must be set")
	}
	if err := cfg.validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (cfg Config) validate() error {
	if cfg.UserCount <= 0 {
		return fmt.Errorf("user count must be positive")
	}
	if cfg.HomesPerUser <= 0 {
		return fmt.Errorf("homes per user must be positive")
	}
	if cfg.DevicesPerHome <= 0 {
		return fmt.Errorf("devices per home must be positive")
	}
	if cfg.DeleteDevicesPerUser > cfg.DevicesPerHome {
		return fmt.Errorf("delete devices per user cannot exceed devices per home")
	}
	if cfg.DeleteHomesPerUser > cfg.HomesPerUser {
		return fmt.Errorf("delete homes per user cannot exceed homes per user")
	}
	if cfg.DeleteHomeSlot >= cfg.HomesPerUser {
		return fmt.Errorf("delete home slot must reference an existing home")
	}
	if cfg.DeleteUsersSelfStartIndex+cfg.DeleteUsersSelfCount > cfg.UserCount {
		return fmt.Errorf("self delete range exceeds configured users")
	}
	if cfg.DeleteUsersAdminStartIndex+cfg.DeleteUsersAdminCount > cfg.UserCount {
		return fmt.Errorf("admin delete range exceeds configured users")
	}
	if cfg.Phase.TotalItems < 0 {
		return fmt.Errorf("phase total items cannot be negative")
	}
	if cfg.Phase.Workers <= 0 {
		return fmt.Errorf("phase workers must be positive")
	}
	if cfg.Phase.PeakRPS <= 0 {
		return fmt.Errorf("phase peak RPS must be positive")
	}
	if cfg.Phase.StartRPS < 0 {
		return fmt.Errorf("phase start RPS cannot be negative")
	}
	if cfg.Phase.MaxDuration <= 0 {
		return fmt.Errorf("phase max duration must be positive")
	}
	if cfg.Phase.Name == "delete_users_admin" {
		if cfg.AdminUsername == "" || cfg.AdminPassword == "" {
			return fmt.Errorf("STRESS_ADMIN_USERNAME and STRESS_ADMIN_PASSWORD must be set for delete_users_admin")
		}
	}
	return nil
}

func mustEnv(key string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		panic(fmt.Sprintf("%s must be set", key))
	}
	return value
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func intEnv(fallback int, keys ...string) int {
	value := firstEnv(keys...)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func floatEnv(fallback float64, key string) float64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func durationEnv(fallback string, key string) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		value = fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		panic(fmt.Sprintf("invalid duration for %s: %v", key, err))
	}
	return parsed
}

func secondsEnv(key string) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return 0
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		panic(fmt.Sprintf("invalid seconds for %s: %v", key, err))
	}
	return time.Duration(parsed) * time.Second
}

func millisEnv(fallback int, key string) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return time.Duration(fallback) * time.Millisecond
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return time.Duration(fallback) * time.Millisecond
	}
	return time.Duration(parsed) * time.Millisecond
}
