package oauth

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/iot-backend/internal/db"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func mockPostgresDB(t *testing.T) (sqlmock.Sqlmock, func()) {
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

func TestIsAdminUserReturnsFalseWithoutQueryForZeroID(t *testing.T) {
	previousDB := db.DB
	db.DB = nil
	t.Cleanup(func() {
		db.DB = previousDB
	})

	if IsAdminUser(0) {
		t.Fatal("expected false for zero user id")
	}
}

func TestIsAdminUserReturnsFalseWhenNoAdminRowExists(t *testing.T) {
	mock, cleanup := mockPostgresDB(t)
	t.Cleanup(cleanup)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "admin_users" WHERE user_id = $1`)).
		WithArgs(uint(42)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	if IsAdminUser(42) {
		t.Fatal("expected false when no admin row exists")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected sql behavior: %v", err)
	}
}

func TestIsAdminUserReturnsTrueWhenAdminRowExists(t *testing.T) {
	mock, cleanup := mockPostgresDB(t)
	t.Cleanup(cleanup)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "admin_users" WHERE user_id = $1`)).
		WithArgs(uint(7)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	if !IsAdminUser(7) {
		t.Fatal("expected true when admin row exists")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected sql behavior: %v", err)
	}
}

func TestRequireAdminRejectsNonAdminUsers(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(sessionUserContextKey, SessionUser{UserID: 1, Username: "user", IsAdmin: false})
		c.Next()
	})
	router.GET("/admin", RequireAdmin(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, rec.Code)
	}

	if strings.TrimSpace(rec.Body.String()) != `{"error":"admin access required"}` {
		t.Fatalf("unexpected response body: %q", rec.Body.String())
	}
}

func TestRequireAdminAllowsAdminUsers(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(sessionUserContextKey, SessionUser{UserID: 99, Username: "admin", IsAdmin: true})
		c.Next()
	})
	router.GET("/admin", RequireAdmin(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, rec.Code)
	}
}
