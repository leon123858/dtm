package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/spf13/cobra"

	_ "dtm/migration" // Import your migration package to register migrations

	_ "github.com/lib/pq" // PostgreSQL 驅動程式

	"github.com/pressly/goose/v3"

	"dtm/config"
)

func migrateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "migrate db in web server",
		Long:  `This command migrate db in web server by goose`,
		Run: func(cmd *cobra.Command, args []string) {
			up, _ := cmd.Flags().GetBool("up")
			down, _ := cmd.Flags().GetBool("down")
			// custom connection string
			connectionIp, _ := cmd.Flags().GetString("ip")
			connectionPwd, _ := cmd.Flags().GetString("pwd")

			if up && down {
				cmd.Help()
				return
			}

			// 設定資料庫連接字串
			connStr := "host=localhost user=postgres dbname=postgres port=5432 sslmode=disable TimeZone=Asia/Taipei"
			if dbURL := os.Getenv("DATABASE_URL"); dbURL != "" {
				connStr = dbURL
				log.Printf("Using DATABASE_URL: %s", connStr)
			} else if connectionIp != "" && connectionPwd != "" {
				connStr = fmt.Sprintf("host=%s user=postgres dbname=postgres password=%s port=5432 sslmode=disable TimeZone=Asia/Taipei", connectionIp, connectionPwd)
				log.Printf("Using provided connection string: %s", connStr)
			} else {
				log.Printf("Using default connection string: %s", connStr)
			}

			if err := goose.SetDialect("postgres"); err != nil {
				log.Fatalf("Failed to set goose dialect: %v", err)
			}

			db, err := sql.Open("postgres", connStr)
			if err != nil {
				log.Fatalf("Failed to open database: %v", err)
			}
			defer db.Close()

			// create app schema if not exists
			if _, err := db.Exec(fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", config.AppName)); err != nil {
				log.Fatalf("Failed to create schema: %v", err)
			}

			// set search path to target schema
			if _, err := db.Exec(fmt.Sprintf("SET search_path TO %s", config.AppName)); err != nil {
				log.Fatalf("Failed to set search path: %v", err)
			}

			// Ping 資料庫以確保連接已成功建立
			pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer pingCancel()
			if err := db.PingContext(pingCtx); err != nil {
				log.Fatalf("Failed to ping database: %v", err)
			}
			log.Println("Successfully connected to the database.")

			migrationsDir := "migration" // 或您 Go 遷移檔案目錄的實際路徑
			if up {
				log.Println("Running 'up' migrations...")
				if err := goose.UpContext(context.Background(), db, migrationsDir); err != nil {
					log.Fatalf("Goose UpContext failed: %v", err)
				}
				log.Println("Goose operations completed.")
			} else if down {
				// 您也可以使用其他 goose 指令，例如：
				log.Println("Rolling back('down') the last migration...")
				if err := goose.DownContext(context.Background(), db, migrationsDir); err != nil {
					log.Fatalf("Goose DownContext failed: %v", err)
				}
				log.Println("Goose operations completed.")
			}
			log.Println("Checking migration status...")
			if err := goose.StatusContext(context.Background(), db, migrationsDir); err != nil {
				log.Fatalf("Goose StatusContext failed: %v", err)
			}
		},
	}

	cmd.Flags().BoolP("up", "u", true, "up the version of db")
	cmd.Flags().BoolP("down", "d", false, "down the version of db")

	cmd.Flags().StringP("ip", "i", "", "database ip")
	cmd.Flags().StringP("pwd", "p", "", "database password")

	return cmd
}
