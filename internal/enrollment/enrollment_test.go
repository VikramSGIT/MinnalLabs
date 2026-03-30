package enrollment

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/iot-backend/internal/db"
	"github.com/iot-backend/internal/models"
	"github.com/iot-backend/internal/oauth"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func mockEnrollmentDB(t *testing.T) (sqlmock.Sqlmock, func()) {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock: %v", err)
	}

	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		_ = sqlDB.Close()
		t.Fatalf("open gorm db: %v", err)
	}

	previousDB := db.DB
	db.DB = gormDB

	return mock, func() {
		db.DB = previousDB
		_ = sqlDB.Close()
	}
}

func enrollmentTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("session_user", oauth.SessionUser{UserID: 7, Username: "tester"})
		c.Next()
	})
	router.GET("/homes", listHomes)
	router.POST("/device", enrollDevice)

	return router
}

func homeRows() []string {
	return []string{
		"id",
		"created_at",
		"updated_at",
		"deleted_at",
		"user_id",
		"name",
		"wifi_ssid",
		"wifi_password",
		"mqtt_username",
		"mqtt_password",
		"mqtt_provision_state",
		"mqtt_provision_error",
		"mqtt_provisioned_at",
	}
}

func validDevicePublicKey(t *testing.T) string {
	t.Helper()

	privateKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate device key: %v", err)
	}

	return base64.StdEncoding.EncodeToString(privateKey.PublicKey().Bytes())
}

func TestListHomesIncludesMQTTProvisionState(t *testing.T) {
	mock, cleanup := mockEnrollmentDB(t)
	t.Cleanup(cleanup)

	now := time.Now().UTC()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "homes" WHERE user_id = $1 AND "homes"."deleted_at" IS NULL ORDER BY name ASC, id ASC`)).
		WithArgs(uint(7)).
		WillReturnRows(sqlmock.NewRows(homeRows()).AddRow(
			11, now, now, nil, 7, "Main House", "ssid", "wifi-pass", "home_7_11", "mqtt-pass",
			models.HomeMQTTProvisionStatePending, "still provisioning", nil,
		))

	req := httptest.NewRequest(http.MethodGet, "/homes", nil)
	rec := httptest.NewRecorder()
	enrollmentTestRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var payload []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(payload) != 1 {
		t.Fatalf("expected 1 home, got %d", len(payload))
	}
	if got := payload[0]["mqtt_provision_state"]; got != models.HomeMQTTProvisionStatePending {
		t.Fatalf("expected mqtt_provision_state %q, got %#v", models.HomeMQTTProvisionStatePending, got)
	}
	if got := payload[0]["mqtt_provision_error"]; got != "still provisioning" {
		t.Fatalf("expected mqtt_provision_error, got %#v", got)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected sql behavior: %v", err)
	}
}

func TestEnrollDeviceRejectsFailedHome(t *testing.T) {
	mock, cleanup := mockEnrollmentDB(t)
	t.Cleanup(cleanup)

	now := time.Now().UTC()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "homes" WHERE "homes"."id" = $1 AND "homes"."deleted_at" IS NULL ORDER BY "homes"."id" LIMIT $2`)).
		WithArgs(uint(19), 1).
		WillReturnRows(sqlmock.NewRows(homeRows()).AddRow(
			19, now, now, nil, 7, "Failed Home", "ssid", "wifi-pass", "home_7_19", "mqtt-pass",
			models.HomeMQTTProvisionStateFailed, "dynsec failed", nil,
		))

	body := bytes.NewBufferString(`{"home_id":19,"name":"Sensor","product_id":1,"product_name":"Smart Sensor","device_public_key":"` + validDevicePublicKey(t) + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/device", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	enrollmentTestRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d", http.StatusConflict, rec.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got := payload["mqtt_provision_state"]; got != models.HomeMQTTProvisionStateFailed {
		t.Fatalf("expected mqtt_provision_state %q, got %#v", models.HomeMQTTProvisionStateFailed, got)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected sql behavior: %v", err)
	}
}

func TestEnrollDeviceRejectsHomeWithoutMQTTCredentials(t *testing.T) {
	mock, cleanup := mockEnrollmentDB(t)
	t.Cleanup(cleanup)

	now := time.Now().UTC()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "homes" WHERE "homes"."id" = $1 AND "homes"."deleted_at" IS NULL ORDER BY "homes"."id" LIMIT $2`)).
		WithArgs(uint(27), 1).
		WillReturnRows(sqlmock.NewRows(homeRows()).AddRow(
			27, now, now, nil, 7, "Pending Home", "ssid", "wifi-pass", "", "",
			models.HomeMQTTProvisionStatePending, "", nil,
		))

	body := bytes.NewBufferString(`{"home_id":27,"name":"Sensor","product_id":1,"product_name":"Smart Sensor","device_public_key":"` + validDevicePublicKey(t) + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/device", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	enrollmentTestRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d", http.StatusConflict, rec.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got := payload["error"]; got != "home mqtt credentials are not ready" {
		t.Fatalf("unexpected error: %#v", got)
	}
	if got := payload["mqtt_provision_state"]; got != models.HomeMQTTProvisionStatePending {
		t.Fatalf("expected mqtt_provision_state %q, got %#v", models.HomeMQTTProvisionStatePending, got)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected sql behavior: %v", err)
	}
}
