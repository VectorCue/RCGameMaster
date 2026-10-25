package database

import (
	"log"

	"gorm.io/gorm"
)

func InitDB(db *gorm.DB) {
	if err := db.AutoMigrate(); err != nil {
		log.Fatalf("Error migrating database: %v", err)
	}
}
