package deque

import (
	"fmt"
	"time"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

var ErrNotFound = fmt.Errorf("record not found")

type DB struct {
	db  *gorm.DB
	loc *time.Location
	log *zap.SugaredLogger
}

type Question struct {
	gorm.Model
	SendAt  time.Time
	Content string
}

type Stats struct {
	TotalQuestions   int64
	FutureQuestions  int64
	PastQuestions    int64
	UpcomingQuestion Question // Next scheduled question
}

func LoadDatabase(path string, loc *time.Location) (*DB, bool) {
	log := zap.L().Named("db").Sugar()
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		log.Error(err)
		return nil, false
	}

	err = db.AutoMigrate(&Question{})
	if err != nil {
		log.Error(err)
		return nil, false
	}

	return &DB{
		db:  db,
		loc: loc,
		log: log,
	}, true
}

func (db *DB) Now() time.Time {
	return time.Now().In(db.loc)
}

// AddQuestion adds a new question to the database and returns the saved question
func (db *DB) AddQuestion(q Question) (Question, error) {
	result := db.db.Create(&q)
	if result.Error != nil {
		db.log.Errorw("failed to add question",
			"error", result.Error,
			"question", q)
		return Question{}, result.Error
	}
	return q, nil
}

func (db *DB) GetQuestionByID(id uint) (Question, error) {
	var question Question
	result := db.db.First(&question, id)

	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return Question{}, ErrNotFound
		}
		db.log.Errorw("failed to get question by id",
			"error", result.Error,
			"id", id)
		return Question{}, result.Error
	}

	return question, nil
}

// LoadFutureQuestions retrieves all questions scheduled for future dates
func (db *DB) LoadFutureQuestions() []Question {
	var questions []Question
	result := db.db.Where("send_at > ?", db.Now()).Find(&questions)
	if result.Error != nil {
		db.log.Errorw("failed to load future questions",
			"error", result.Error)
		return []Question{}
	}
	return questions
}

// NextEmptyDate finds the next available date that doesn't have a scheduled question
func (db *DB) NextEmptyDate(hour, minute int, location *time.Location) time.Time {
	now := time.Now()

	// Start with today if the scheduled time is still in the future
	proposedDate := time.Date(
		now.Year(),
		now.Month(),
		now.Day(),
		hour, minute, 0, 0,
		location,
	)

	// Otherwise start with tomorrow
	if proposedDate.Before(now) {
		proposedDate = proposedDate.AddDate(0, 0, 1)
	}

	for {
		var count int64
		// Check if there's any question scheduled for the proposed date
		result := db.db.Model(&Question{}).
			Where("DATE(send_at) = DATE(?)", proposedDate).
			Count(&count)

		if result.Error != nil {
			db.log.Errorw("failed to check date availability",
				"error", result.Error,
				"date", proposedDate)
			// Return tomorrow with specified time in case of error
			tomorrow := now.AddDate(0, 0, 1)
			return time.Date(
				tomorrow.Year(),
				tomorrow.Month(),
				tomorrow.Day(),
				hour, minute, 0, 0,
				location,
			)
		}

		// If no questions are scheduled for this date, return it
		if count == 0 {
			return proposedDate
		}

		// Try the next day
		proposedDate = proposedDate.AddDate(0, 0, 1)
	}
}

// LoadStats retrieves statistics about questions in the database
func (db *DB) LoadStats() (Stats, error) {
	var stats Stats

	// Get total questions count
	result := db.db.Model(&Question{}).Count(&stats.TotalQuestions)
	if result.Error != nil {
		return stats, result.Error
	}

	// Get future questions count
	result = db.db.Model(&Question{}).
		Where("send_at > ?", db.Now()).
		Count(&stats.FutureQuestions)
	if result.Error != nil {
		return stats, result.Error
	}

	// Get past questions count
	result = db.db.Model(&Question{}).
		Where("send_at <= ?", db.Now()).
		Count(&stats.PastQuestions)
	if result.Error != nil {
		return stats, result.Error
	}

	// Get next upcoming question
	result = db.db.Where("send_at > ?", db.Now()).
		Order("send_at ASC").
		First(&stats.UpcomingQuestion)
	if result.Error != nil && result.Error != gorm.ErrRecordNotFound {
		return stats, result.Error
	}

	return stats, nil
}

// DeleteQuestion deletes a question from the database
func (db *DB) DeleteQuestion(id uint) error {
	result := db.db.Delete(&Question{}, id)
	if result.Error != nil {
		db.log.Errorw("failed to delete question",
			"error", result.Error,
			"id", id)
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
