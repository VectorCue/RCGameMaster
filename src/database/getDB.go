package database

import (
	"RCJVGameMaster/src/config"
	"log"
	"net"
	"net/url"
	"strconv"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func GetDB(cfg *config.Config) *gorm.DB {
	sslmode := cfg.Database.SSLMode
	if sslmode == "" {
		sslmode = "disable"
	}
	query := url.Values{"sslmode": {sslmode}}
	dsn := url.URL{Scheme: "postgres", User: url.UserPassword(cfg.Database.User, cfg.Database.Password),
		Host: net.JoinHostPort(cfg.Database.Host, strconv.Itoa(cfg.Database.Port)), Path: "/" + cfg.Database.Database, RawQuery: query.Encode()}

	// Open connection to database
	db, err := gorm.Open(postgres.Open(dsn.String()), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
		Logger:                                   logger.Default.LogMode(logger.Error),
	})
	if err != nil {
		log.Fatalf("Error connecting to database: %v\n", err)
	}

	return db
}
