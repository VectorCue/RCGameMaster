package database

import (
	"RCJVGameMaster/src/domain"
	"log"

	"gorm.io/gorm"
)

func InitDB(db *gorm.DB) {
	if err := db.AutoMigrate(domain.Models()...); err != nil {
		log.Fatalf("Error migrating database: %v", err)
	}
}
