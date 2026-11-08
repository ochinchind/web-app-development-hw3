package db

import (
	"context"
	"fmt"
	"log"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"hr-service/core/internal/models"
)

func Connect(ctx context.Context, dsn string) (*gorm.DB, error) {
	var lastErr error
	for i := 0; i < 30; i++ {
		db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
			Logger:  logger.Default.LogMode(logger.Warn),
			NowFunc: func() time.Time { return time.Now().UTC() },
		})
		if err == nil {
			return db, nil
		}
		lastErr = err
		log.Printf("waiting for database: %v", lastErr)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return nil, fmt.Errorf("database not reachable: %w", lastErr)
}

func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&models.Department{},
		&models.WorkSchedule{},
		&models.Employee{},
		&models.PerformanceReview{},
		&models.AttendanceRecord{},
		&models.Payslip{},
		&models.Leave{},
	); err != nil {
		return fmt.Errorf("auto-migrate: %w", err)
	}
	log.Print("database schema is up to date")
	return nil
}
