package pg

import (
	"dtm/config"
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
		host := "127.0.0.1"
		if os.Getenv("DATABASE_HOST") != "" {
			host = os.Getenv("DATABASE_HOST")
		}
		connStr = fmt.Sprintf("host=%s user=%s dbname=postgres password=%s port=5432 sslmode=disable", host, dbUser, os.Getenv("DATABASE_PASSWORD"))
		log.Printf("Using DATABASE_PASSWORD: *")
	} else {
		log.Printf("Using default connection string: %s", connStr)
	}

	// dsn should point to target schema for the app
	appSchema := config.AppName
	connStr += fmt.Sprintf(" search_path=%s", appSchema)

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
