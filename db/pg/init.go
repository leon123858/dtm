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
		log.Printf("Using DATABASE_URL: *")
	} else if os.Getenv("DATABASE_PASSWORD") != "" {
		dbUser := "postgres"
		if os.Getenv("DATABASE_USER") != "" {
			dbUser = os.Getenv("DATABASE_USER")
		}
		region := "Asia/Taipei"
		if os.Getenv("DATABASE_REGION") != "" {
			region = os.Getenv("DATABASE_REGION")
		}
		host := "localhost"
		if os.Getenv("DATABASE_HOST") != "" {
			host = os.Getenv("DATABASE_HOST")
		}
		connStr = fmt.Sprintf("host=%s user=%s dbname=postgres password=%s port=5432 sslmode=disable TimeZone=%s", host, dbUser, os.Getenv("DATABASE_PASSWORD"), region)
		log.Printf("Using DATABASE_PASSWORD: *")
	} else if os.Getenv("CLOUD_SQL_SA_EMAIL") != "" {
		email := os.Getenv("CLOUD_SQL_SA_EMAIL")
		region := "Asia/Taipei"
		if os.Getenv("DATABASE_REGION") != "" {
			region = os.Getenv("DATABASE_REGION")
		}
		connStr = fmt.Sprintf("host=127.0.0.1 user=%s dbname=postgres port=5432 sslmode=disable TimeZone=%s", email, region)
	} else {
		log.Printf("Using default connection string: %s", connStr)
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

	// log.Println("PostgreSQL GORM connection initialized successfully!")
	return db, nil
}
