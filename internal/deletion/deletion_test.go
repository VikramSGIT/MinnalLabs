package deletion

import (
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/iot-backend/internal/db"
	"github.com/iot-backend/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func mockDeletionDB(t *testing.T) (sqlmock.Sqlmock, *gorm.DB, func()) {
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

	return mock, gormDB, func() {
		db.DB = previousDB
		_ = sqlDB.Close()
	}
}

func TestScheduleHomeDeletionMarksDeletingAndQueuesCleanup(t *testing.T) {
	mock, gormDB, cleanup := mockDeletionDB(t)
	t.Cleanup(cleanup)

	now := time.Date(2026, time.March, 31, 12, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "homes" SET .* WHERE \(id = \$\d+ AND deleted_at IS NULL\) AND "homes"\."deleted_at" IS NULL`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "home_mqtt_jobs" WHERE home_id = $1`)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`INSERT INTO "home_mqtt_jobs" .* RETURNING "id"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectCommit()

	if err := gormDB.Transaction(func(tx *gorm.DB) error {
		return ScheduleHomeDeletion(tx, 11, now)
	}); err != nil {
		t.Fatalf("ScheduleHomeDeletion() error = %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected sql behavior: %v", err)
	}
}

func TestScheduleUserDeletionSoftDeletesUserAndQueuesHomes(t *testing.T) {
	mock, gormDB, cleanup := mockDeletionDB(t)
	t.Cleanup(cleanup)

	now := time.Date(2026, time.March, 31, 12, 0, 0, 0, time.UTC)
	userRows := sqlmock.NewRows([]string{"id", "created_at", "updated_at", "deleted_at", "username", "password"}).
		AddRow(7, now, now, nil, "tester", "hash")
	homeRows := sqlmock.NewRows([]string{"id"}).AddRow(21)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "users" WHERE "users"."id" = $1 AND "users"."deleted_at" IS NULL ORDER BY "users"."id" LIMIT $2`)).
		WithArgs(uint(7), 1).
		WillReturnRows(userRows)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT "id" FROM "homes" WHERE (user_id = $1 AND deleted_at IS NULL) AND "homes"."deleted_at" IS NULL ORDER BY id ASC`)).
		WithArgs(uint(7)).
		WillReturnRows(homeRows)
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "admin_users" WHERE user_id = $1`)).
		WithArgs(uint(7)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "users" SET "deleted_at"=\$1 WHERE "users"\."id" = \$2 AND "users"\."deleted_at" IS NULL`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "homes" SET .* WHERE \(id = \$\d+ AND deleted_at IS NULL\) AND "homes"\."deleted_at" IS NULL`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "home_mqtt_jobs" WHERE home_id = $1`)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`INSERT INTO "home_mqtt_jobs" .* RETURNING "id"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectCommit()

	result, err := ScheduleUserDeletion(gormDB, 7, now)
	if err != nil {
		t.Fatalf("ScheduleUserDeletion() error = %v", err)
	}
	if result.UserID != 7 {
		t.Fatalf("expected deleted user id 7, got %d", result.UserID)
	}
	if len(result.HomeIDs) != 1 || result.HomeIDs[0] != 21 {
		t.Fatalf("unexpected queued home ids: %#v", result.HomeIDs)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected sql behavior: %v", err)
	}
}

func TestHomeAllowsDeviceProvisioningRejectsDeletingState(t *testing.T) {
	home := models.Home{MQTTProvisionState: models.HomeMQTTProvisionStateDeleting}
	if home.AllowsDeviceProvisioning() {
		t.Fatal("expected deleting home to reject device provisioning")
	}
}
