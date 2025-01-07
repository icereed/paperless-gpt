package main

import (
	"os"
	"path/filepath"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// ModificationHistory represents the schema of the modification_history table
type ModificationHistory struct {
	ID            uint   `gorm:"primaryKey"`             // Auto-incrementing primary key
	DocumentID    uint   `gorm:"not null"`               // Foreign key to documents table (if applicable)
	DateChanged   string `gorm:"not null"`               // Date and time of modification
	ModField      string `gorm:"size:255;not null"`      // Field being modified
	PreviousValue string `gorm:"size:1048576"`           // Previous value of the field
	NewValue      string `gorm:"size:1048576"`           // New value of the field
	Undone        bool   `gorm:"not null;default:false"` // Whether the modification has been undone
	UndoneDate    string `gorm:"default:null"`           // Date and time of undoing the modification
}

// InitializeDB initializes the SQLite database and migrates the schema
func InitializeDB() *gorm.DB {
	// Ensure db directory exists
	dbDir := "db"
	if err := os.MkdirAll(dbDir, os.ModePerm); err != nil {
		log.Fatalf("Failed to create db directory: %v", err)
	}

	dbPath := filepath.Join(dbDir, "modification_history.db")

	// Connect to SQLite database
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// Migrate the schema (create the table if it doesn't exist)
	err = db.AutoMigrate(&ModificationHistory{})
	if err != nil {
		log.Fatalf("Failed to migrate database schema: %v", err)
	}

	return db
}

// InsertModification inserts a new modification record into the database
func InsertModification(db *gorm.DB, record *ModificationHistory) error {
	log.Debugf("Passed modification record: %+v", record)
	record.DateChanged = time.Now().Format(time.RFC3339) // Set the DateChanged field to the current time
	log.Debugf("Inserting modification record: %+v", record)
	result := db.Create(&record) // GORM's Create method
	log.Debugf("Insertion result: %+v", result)
	return result.Error
}

// GetModification retrieves a modification record by its ID
func GetModification(db *gorm.DB, id uint) (*ModificationHistory, error) {
	var record ModificationHistory
	result := db.First(&record, id) // GORM's First method retrieves the first record matching the ID
	return &record, result.Error
}

// GetAllModifications retrieves all modification records from the database
func GetAllModifications(db *gorm.DB) ([]ModificationHistory, error) {
	var records []ModificationHistory
	result := db.Order("date_changed DESC").Find(&records) // GORM's Find method retrieves all records
	return records, result.Error
}

// UndoModification marks a modification record as undone and sets the undo date
func SetModificationUndone(db *gorm.DB, record *ModificationHistory) error {
	record.Undone = true
	record.UndoneDate = time.Now().Format(time.RFC3339)
	result := db.Save(&record) // GORM's Save method
	return result.Error
}
