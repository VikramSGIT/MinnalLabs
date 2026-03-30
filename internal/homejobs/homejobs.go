package homejobs

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/iot-backend/internal/models"
	"github.com/iot-backend/internal/mqtt"
	"github.com/iot-backend/internal/state"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	WorkerInterval                = 10 * time.Second
	// Process a larger batch each tick so bursty home creation still respects the
	// 10s queue delay without forcing readiness past the stress-timeout window.
	jobBatchSize                  = 64
	staleClaimAfter               = 2 * time.Minute
	maxProvisionAttempts          = 6
	DeviceMQTTConnectDelaySeconds = 15
)

type Worker struct {
	db *gorm.DB
}

func NewWorker(db *gorm.DB) *Worker {
	return &Worker{db: db}
}

func initialNextRunAt(now time.Time) time.Time {
	return now.Add(WorkerInterval)
}

func EnqueueProvision(tx *gorm.DB, homeID uint) error {
	return upsertJob(tx, homeID, models.HomeMQTTJobOperationProvision)
}

func ReplaceWithCleanupJob(tx *gorm.DB, homeID uint) error {
	if err := tx.Where("home_id = ?", homeID).Delete(&models.HomeMQTTJob{}).Error; err != nil {
		return fmt.Errorf("clear existing home mqtt jobs: %w", err)
	}
	return upsertJob(tx, homeID, models.HomeMQTTJobOperationCleanup)
}

func upsertJob(tx *gorm.DB, homeID uint, operation string) error {
	now := time.Now().UTC()
	nextRunAt := initialNextRunAt(now)
	job := models.HomeMQTTJob{
		HomeID:    homeID,
		Operation: operation,
		Status:    models.HomeMQTTJobStatusPending,
		Attempts:  0,
		NextRunAt: nextRunAt,
		LastError: "",
	}

	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "home_id"},
			{Name: "operation"},
		},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"status":      models.HomeMQTTJobStatusPending,
			"attempts":    0,
			"next_run_at": nextRunAt,
			"claimed_at":  nil,
			"last_error":  "",
			"updated_at":  now,
		}),
	}).Create(&job).Error
}

func (w *Worker) Start(ctx context.Context) {
	go func() {
		w.runOnce()

		ticker := time.NewTicker(WorkerInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.runOnce()
			}
		}
	}()
}

func (w *Worker) runOnce() {
	if err := w.requeueStaleClaims(); err != nil {
		log.Printf("Home MQTT worker failed to requeue stale jobs: %v", err)
	}

	jobs, err := w.claimJobs()
	if err != nil {
		log.Printf("Home MQTT worker failed to claim jobs: %v", err)
		return
	}

	for _, job := range jobs {
		if err := w.processJob(job); err != nil {
			log.Printf("Home MQTT worker failed processing job %d (%s): %v", job.ID, job.Operation, err)
		}
	}
}

func (w *Worker) requeueStaleClaims() error {
	now := time.Now().UTC()
	return w.db.Model(&models.HomeMQTTJob{}).
		Where("status = ? AND claimed_at IS NOT NULL AND claimed_at < ?", models.HomeMQTTJobStatusRunning, now.Add(-staleClaimAfter)).
		Updates(map[string]interface{}{
			"status":      models.HomeMQTTJobStatusPending,
			"claimed_at":  nil,
			"next_run_at": now,
			"updated_at":  now,
		}).Error
}

func (w *Worker) claimJobs() ([]models.HomeMQTTJob, error) {
	now := time.Now().UTC()
	jobs := make([]models.HomeMQTTJob, 0, jobBatchSize)

	const claimQuery = `
WITH candidate_jobs AS (
	SELECT id
	FROM home_mqtt_jobs
	WHERE status = ? AND next_run_at <= ?
	ORDER BY next_run_at ASC, id ASC
	FOR UPDATE SKIP LOCKED
	LIMIT ?
)
UPDATE home_mqtt_jobs AS jobs
SET
	status = ?,
	claimed_at = ?,
	attempts = jobs.attempts + 1,
	updated_at = ?
FROM candidate_jobs
WHERE jobs.id = candidate_jobs.id
RETURNING
	jobs.id,
	jobs.home_id,
	jobs.operation,
	jobs.status,
	jobs.attempts,
	jobs.next_run_at,
	jobs.claimed_at,
	jobs.last_error,
	jobs.created_at,
	jobs.updated_at
`

	err := w.db.Raw(
		claimQuery,
		models.HomeMQTTJobStatusPending,
		now,
		jobBatchSize,
		models.HomeMQTTJobStatusRunning,
		now,
		now,
	).Scan(&jobs).Error
	return jobs, err
}

func (w *Worker) processJob(job models.HomeMQTTJob) error {
	switch job.Operation {
	case models.HomeMQTTJobOperationProvision:
		return w.processProvisionJob(job)
	case models.HomeMQTTJobOperationCleanup:
		return w.processCleanupJob(job)
	default:
		return w.markJobFailed(job, fmt.Errorf("unknown home mqtt job operation %q", job.Operation))
	}
}

func (w *Worker) processProvisionJob(job models.HomeMQTTJob) error {
	var home models.Home
	if err := w.db.Unscoped().First(&home, job.HomeID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return w.deleteJob(job.ID)
		}
		return w.rescheduleProvision(job, 0, fmt.Errorf("load home: %w", err))
	}

	if home.DeletedAt.Valid || home.MQTTState() == models.HomeMQTTProvisionStateDeleting {
		return w.deleteJob(job.ID)
	}
	if !home.HasMQTTCredentials() {
		return w.rescheduleProvision(job, home.ID, fmt.Errorf("home mqtt credentials are missing"))
	}

	if err := mqtt.CleanupHomeAccess(home.UserID, home.ID, home.MQTTUsername); err != nil {
		return w.rescheduleProvision(job, home.ID, fmt.Errorf("clear stale mqtt access: %w", err))
	}
	if err := mqtt.ProvisionHomeAccess(home.UserID, home.ID, home.MQTTUsername, home.MQTTPassword); err != nil {
		return w.rescheduleProvision(job, home.ID, fmt.Errorf("provision mqtt access: %w", err))
	}

	now := time.Now().UTC()
	return w.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Home{}).
			Where("id = ?", home.ID).
			Updates(map[string]interface{}{
				"mqtt_provision_state": models.HomeMQTTProvisionStateReady,
				"mqtt_provision_error": "",
				"mqtt_provisioned_at":  now,
				"updated_at":           now,
			}).Error; err != nil {
			return fmt.Errorf("mark home mqtt ready: %w", err)
		}

		if err := tx.Delete(&models.HomeMQTTJob{}, job.ID).Error; err != nil {
			return fmt.Errorf("delete provision job: %w", err)
		}
		return nil
	})
}

func (w *Worker) processCleanupJob(job models.HomeMQTTJob) error {
	var home models.Home
	if err := w.db.Unscoped().First(&home, job.HomeID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return w.deleteJob(job.ID)
		}
		return w.rescheduleCleanup(job, 0, fmt.Errorf("load home: %w", err))
	}

	var devices []models.Device
	if err := w.db.Unscoped().
		Where("home_id = ?", home.ID).
		Order("id ASC").
		Find(&devices).Error; err != nil {
		return w.rescheduleCleanup(job, home.ID, fmt.Errorf("load home devices: %w", err))
	}

	if home.MQTTUsername != "" {
		if err := mqtt.CleanupHomeAccess(home.UserID, home.ID, home.MQTTUsername); err != nil {
			return w.rescheduleCleanup(job, home.ID, fmt.Errorf("cleanup mqtt access: %w", err))
		}
	}

	deviceIDs := make([]uint, 0, len(devices))
	for _, device := range devices {
		mqtt.UnsubscribeDevice(device)
		if err := mqtt.ClearRetainedDeviceFirmwareUpdate(device); err != nil {
			return w.rescheduleCleanup(job, home.ID, fmt.Errorf("clear retained ota for device %d: %w", device.ID, err))
		}
		deviceIDs = append(deviceIDs, device.ID)
	}

	if err := w.db.Transaction(func(tx *gorm.DB) error {
		if len(deviceIDs) > 0 {
			if err := tx.Unscoped().Where("id IN ?", deviceIDs).Delete(&models.Device{}).Error; err != nil {
				return fmt.Errorf("hard delete devices: %w", err)
			}
		}
		if err := tx.Unscoped().Delete(&models.Home{}, home.ID).Error; err != nil {
			return fmt.Errorf("hard delete home: %w", err)
		}
		return nil
	}); err != nil {
		return w.rescheduleCleanup(job, home.ID, err)
	}

	if len(deviceIDs) > 0 {
		state.RemoveDevices(deviceIDs)
	}
	return nil
}

func (w *Worker) deleteJob(jobID uint) error {
	return w.db.Delete(&models.HomeMQTTJob{}, jobID).Error
}

func (w *Worker) rescheduleProvision(job models.HomeMQTTJob, homeID uint, failure error) error {
	now := time.Now().UTC()
	jobState := models.HomeMQTTProvisionStatePending
	jobUpdates := map[string]interface{}{
		"last_error": failure.Error(),
		"claimed_at": nil,
		"updated_at": now,
	}

	if job.Attempts >= maxProvisionAttempts {
		jobState = models.HomeMQTTProvisionStateFailed
		jobUpdates["status"] = models.HomeMQTTJobStatusFailed
	} else {
		jobUpdates["status"] = models.HomeMQTTJobStatusPending
		jobUpdates["next_run_at"] = now.Add(retryDelay(job.Attempts))
	}

	return w.db.Transaction(func(tx *gorm.DB) error {
		if homeID != 0 {
			if err := tx.Model(&models.Home{}).
				Where("id = ?", homeID).
				Updates(map[string]interface{}{
					"mqtt_provision_state": jobState,
					"mqtt_provision_error": failure.Error(),
					"updated_at":           now,
				}).Error; err != nil {
				return fmt.Errorf("update home mqtt state: %w", err)
			}
		}
		if err := tx.Model(&models.HomeMQTTJob{}).
			Where("id = ?", job.ID).
			Updates(jobUpdates).Error; err != nil {
			return fmt.Errorf("update provision job: %w", err)
		}
		return nil
	})
}

func (w *Worker) rescheduleCleanup(job models.HomeMQTTJob, homeID uint, failure error) error {
	now := time.Now().UTC()
	return w.db.Transaction(func(tx *gorm.DB) error {
		if homeID != 0 {
			if err := tx.Unscoped().Model(&models.Home{}).
				Where("id = ?", homeID).
				Updates(map[string]interface{}{
					"mqtt_provision_state": models.HomeMQTTProvisionStateDeleting,
					"mqtt_provision_error": failure.Error(),
					"updated_at":           now,
				}).Error; err != nil {
				return fmt.Errorf("update deleting home state: %w", err)
			}
		}
		if err := tx.Model(&models.HomeMQTTJob{}).
			Where("id = ?", job.ID).
			Updates(map[string]interface{}{
				"status":      models.HomeMQTTJobStatusPending,
				"claimed_at":  nil,
				"last_error":  failure.Error(),
				"next_run_at": now.Add(retryDelay(job.Attempts)),
				"updated_at":  now,
			}).Error; err != nil {
			return fmt.Errorf("update cleanup job: %w", err)
		}
		return nil
	})
}

func (w *Worker) markJobFailed(job models.HomeMQTTJob, failure error) error {
	now := time.Now().UTC()
	return w.db.Model(&models.HomeMQTTJob{}).
		Where("id = ?", job.ID).
		Updates(map[string]interface{}{
			"status":     models.HomeMQTTJobStatusFailed,
			"claimed_at": nil,
			"last_error": failure.Error(),
			"updated_at": now,
		}).Error
}

func retryDelay(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	if attempts > 6 {
		attempts = 6
	}
	return time.Duration(attempts) * WorkerInterval
}
