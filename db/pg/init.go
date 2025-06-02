package pg

import (
	"fmt"
	"log"
	"os"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func CreateDSN() string {
	connStr := "host=localhost user=postgres dbname=postgres port=5432 sslmode=disable TimeZone=Asia/Taipei"
	if os.Getenv("DATABASE_URL") != "" {
		connStr = os.Getenv("DATABASE_URL")
	}
	return connStr
}

func CloseGORM(db *gorm.DB) {
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("Error getting underlying sql.DB from GORM: %v", err)
	}
	sqlDB.Close()
}

// InitPostgresGORM initializes a new GORM DB connection to PostgreSQL.
// It also performs auto-migration for the defined models.
func InitPostgresGORM(dsn string) (*gorm.DB, error) {
	newLogger := logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags), // io writer
		logger.Config{
			SlowThreshold: time.Second,   // Slow SQL threshold
			LogLevel:      logger.Silent, // Log level (Silent, Error, Warn, Info)
			Colorful:      true,          // Disable color
		},
	)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: newLogger, // Apply the custom logger
		// DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Ping the database to ensure connection is alive
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}
	if err = sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	log.Println("PostgreSQL GORM connection initialized successfully!")
	return db, nil
}
