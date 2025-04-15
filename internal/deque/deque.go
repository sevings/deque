package deque

import (
	"fmt"
	"strings"
	"time"
)

type AskFunc func(q Question)

type Deque struct {
	db      *DB
	cfg     Config
	sched   Scheduler
	askFunc AskFunc
}

func NewDeque(db *DB, cfg Config, sched Scheduler) *Deque {
	d := &Deque{
		db:    db,
		cfg:   cfg,
		sched: sched,
	}
	return d
}

func (d *Deque) SetAskFunc(fn AskFunc) {
	d.askFunc = fn
	d.sched.SetJobFunc(func(id JobID) {
		question, err := d.db.GetQuestionByID(uint(id))
		if err != nil {
			// If we can't load the question, we can't do anything else
			return
		}
		d.askFunc(question)
	})
}

// Start loads all future questions from the database and schedules them
func (d *Deque) Start() error {
	if d.askFunc == nil {
		return fmt.Errorf("ask function not set")
	}

	questions := d.db.LoadFutureQuestions()
	for _, q := range questions {
		d.sched.Schedule(q.SendAt, JobID(q.ID))
	}

	return nil
}

func (d *Deque) ScheduleQuestions(text string) error {
	lines := strings.Split(text, "\n")

	defaultTime, err := time.Parse("15:04", d.cfg.DefaultTime)
	if err != nil {
		return fmt.Errorf("invalid default time format: %w", err)
	}
	location, err := time.LoadLocation(d.cfg.Location)
	if err != nil {
		return fmt.Errorf("invalid location: %w", err)
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Parse date and time if present
		var scheduledTime time.Time
		var content string

		// Split the line into parts
		parts := strings.SplitN(line, " ", 3)
		// Try to parse first part as date (MM.DD)
		hasDate := false
		if len(parts) > 1 {
			date, err := time.Parse("02.01", parts[0])
			// If it looks like a date format (contains a dot) but fails to parse, return error
			if strings.Contains(parts[0], ".") && err != nil {
				return fmt.Errorf("invalid date format: %w", err)
			}
			if err == nil {
				currentYear := time.Now().Year()
				scheduledTime = time.Date(currentYear, date.Month(), date.Day(),
					defaultTime.Hour(), defaultTime.Minute(), 0, 0, location)

				// Only push to next year if the date is strictly before today
				today := time.Now().In(location).Truncate(24 * time.Hour)
				if scheduledTime.Before(today) {
					scheduledTime = scheduledTime.AddDate(1, 0, 0)
				}

				hasDate = true
				parts = parts[1:] // Remove the date part
			}
		}
		if !hasDate {
			scheduledTime = d.db.NextEmptyDate()
		}

		// Try to parse time (HH:MM)
		hasTime := false
		if len(parts) > 1 {
			if t, err := time.Parse("15:04", parts[0]); err == nil {
				scheduledTime = time.Date(scheduledTime.Year(), scheduledTime.Month(),
					scheduledTime.Day(), t.Hour(), t.Minute(), 0, 0, location)
				hasTime = true
				parts = parts[1:] // Remove the time part
			}
		}
		if !hasTime {
			scheduledTime = time.Date(scheduledTime.Year(), scheduledTime.Month(),
				scheduledTime.Day(), defaultTime.Hour(), defaultTime.Minute(), 0, 0, location)
		}

		// The remaining parts form the content
		content = strings.Join(parts, " ")

		// Create and save the question
		question, err := d.db.AddQuestion(Question{
			SendAt:  scheduledTime,
			Content: content,
		})
		if err != nil {
			return fmt.Errorf("failed to add question: %w", err)
		}

		// Schedule the question
		d.sched.Schedule(scheduledTime, JobID(question.ID))
	}

	return nil
}
