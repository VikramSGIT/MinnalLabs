package deletion

import (
	"fmt"
	"time"

	"github.com/iot-backend/internal/homejobs"
	"github.com/iot-backend/internal/models"
	"gorm.io/gorm"
)

type UserDeletionResult struct {
	UserID  uint
	HomeIDs []uint
}

func ScheduleHomeDeletion(tx *gorm.DB, homeID uint, now time.Time) error {
	if tx == nil {
		return fmt.Errorf("transaction is required")
	}
	if homeID == 0 {
		return fmt.Errorf("home id is required")
	}

	if err := tx.Model(&models.Home{}).
		Where("id = ? AND deleted_at IS NULL", homeID).
		Updates(map[string]interface{}{
			"mqtt_provision_state": models.HomeMQTTProvisionStateDeleting,
			"mqtt_provision_error": "",
			"updated_at":           now,
		}).Error; err != nil {
		return fmt.Errorf("mark home deleting: %w", err)
	}

	if err := homejobs.ReplaceWithCleanupJob(tx, homeID); err != nil {
		return fmt.Errorf("enqueue home cleanup: %w", err)
	}

	return nil
}

func ScheduleUserDeletion(db *gorm.DB, userID uint, now time.Time) (UserDeletionResult, error) {
	result := UserDeletionResult{UserID: userID}
	if db == nil {
		return result, fmt.Errorf("database is required")
	}
	if userID == 0 {
		return result, fmt.Errorf("user id is required")
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		var user models.User
		if err := tx.First(&user, userID).Error; err != nil {
			return fmt.Errorf("load user: %w", err)
		}

		if err := tx.Model(&models.Home{}).
			Where("user_id = ? AND deleted_at IS NULL", userID).
			Order("id ASC").
			Pluck("id", &result.HomeIDs).Error; err != nil {
			return fmt.Errorf("load user homes: %w", err)
		}

		if err := tx.Where("user_id = ?", userID).Delete(&models.AdminUser{}).Error; err != nil {
			return fmt.Errorf("remove admin membership: %w", err)
		}

		if err := tx.Delete(&user).Error; err != nil {
			return fmt.Errorf("soft delete user: %w", err)
		}

		for _, homeID := range result.HomeIDs {
			if err := ScheduleHomeDeletion(tx, homeID, now); err != nil {
				return err
			}
		}

		return nil
	})
	return result, err
}
