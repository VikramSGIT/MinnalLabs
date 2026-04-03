package db

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/iot-backend/internal/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var DB *gorm.DB

func InitDB(cfg *config.Config) {
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=UTC",
		cfg.Database.Host,
		cfg.Database.User,
		cfg.Database.Password,
		cfg.Database.Name,
		cfg.Database.Port,
	)

	var err error
	DB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	sqlDB, err := DB.DB()
	if err != nil {
		log.Fatalf("Failed to get underlying sql.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(150)
	sqlDB.SetMaxIdleConns(75)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)
	sqlDB.SetConnMaxIdleTime(2 * time.Minute)

	log.Println("Connected to database successfully")
}

func RunMigrations() {
	DB.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		filename VARCHAR(255) PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`)

	entries, err := os.ReadDir("migrations")
	if err != nil {
		log.Fatalf("Failed to read migrations directory: %v", err)
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		files = append(files, filepath.Join("migrations", entry.Name()))
	}
	sort.Strings(files)

	applied := 0
	for _, path := range files {
		name := filepath.Base(path)
		var count int64
		DB.Raw("SELECT COUNT(*) FROM schema_migrations WHERE filename = ?", name).Scan(&count)
		if count > 0 {
			continue
		}

		sqlBytes, err := os.ReadFile(path)
		if err != nil {
			log.Fatalf("Failed to read migration file %s: %v", path, err)
		}
		if err := DB.Exec(string(sqlBytes)).Error; err != nil {
			log.Fatalf("Failed to run migration %s: %v", path, err)
		}
		DB.Exec("INSERT INTO schema_migrations (filename) VALUES (?)", name)
		log.Printf("Applied migration %s", path)
		applied++
	}

	log.Printf("Database migrations completed (%d applied, %d already up to date)", applied, len(files)-applied)
}

func RunSQLFile(path string) {
	sqlBytes, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("Failed to read SQL file %s: %v", path, err)
	}
	if err := DB.Exec(string(sqlBytes)).Error; err != nil {
		log.Fatalf("Failed to run SQL file %s: %v", path, err)
	}
	log.Printf("Applied SQL file %s", path)
}
