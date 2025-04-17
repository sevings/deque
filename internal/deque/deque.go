package deque

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type AskFunc func(q string)

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

		text := question.Content
		if d.cfg.Format != "" {
			text = fmt.Sprintf(d.cfg.Format, text)
		}
		d.askFunc(text)
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
		var hour, minute int

		// Set default hour and minute
		hour = defaultTime.Hour()
		minute = defaultTime.Minute()

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
					hour, minute, 0, 0, location)

				// Only push to next year if the date is strictly before today
				today := time.Now().In(location).Truncate(24 * time.Hour)
				if scheduledTime.Before(today) {
					scheduledTime = scheduledTime.AddDate(1, 0, 0)
				}

				hasDate = true
				parts = parts[1:] // Remove the date part
			}
		}

		// Try to parse time (HH:MM)
		if len(parts) > 1 {
			if t, err := time.Parse("15:04", parts[0]); err == nil {
				hour = t.Hour()
				minute = t.Minute()

				if hasDate {
					// Update the time part of scheduledTime
					scheduledTime = time.Date(scheduledTime.Year(), scheduledTime.Month(),
						scheduledTime.Day(), hour, minute, 0, 0, location)
				}

				parts = parts[1:] // Remove the time part
			}
		}

		if !hasDate {
			scheduledTime = d.db.NextEmptyDate(hour, minute, location)
		}

		content = strings.Join(parts, " ")

		question, err := d.db.AddQuestion(Question{
			SendAt:  scheduledTime,
			Content: content,
		})
		if err != nil {
			return fmt.Errorf("failed to add question: %w", err)
		}

		d.sched.Schedule(scheduledTime, JobID(question.ID))
	}

	return nil
}

func (d *Deque) GetHelp() string {
	return `Инструкция по планированию вопросов:

1. Каждый вопрос должен быть на отдельной строке
2. Формат строки: [ДД.ММ] [ЧЧ:ММ] текст вопроса
   - Дата и время опциональны
   - Если дата не указана, вопрос будет запланирован на следующую свободную дату
   - Если время не указано, будет использовано время по умолчанию

Примеры:
25.12 15:30 Какой подарок вы хотите на Новый год?
10:00 Как у вас дела сегодня?
Что вы думаете о погоде?`
}

func (d *Deque) GetStats() (string, error) {
	stats, err := d.db.LoadStats()
	if err != nil {
		return "", err
	}

	statsText := fmt.Sprintf(`📊 Статистика вопросов:

Всего вопросов: %d
├ Прошедших: %d
└ Предстоящих: %d`,
		stats.TotalQuestions,
		stats.PastQuestions,
		stats.FutureQuestions)

	if stats.FutureQuestions > 0 {
		statsText += fmt.Sprintf(`

Следующий вопрос:
📅 %s
❔ %s`,
			stats.UpcomingQuestion.SendAt.Format("02.01.2006 15:04"),
			stats.UpcomingQuestion.Content)
	} else {
		statsText += "\n\nНет запланированных вопросов."
	}

	return statsText, nil
}

// GetFutureQuestions returns future questions formatted as strings, split into blocks of 15
func (d *Deque) GetFutureQuestions() ([]string, error) {
	// Get future questions from DB
	questions := d.db.LoadFutureQuestions()
	if len(questions) == 0 {
		return []string{"Нет запланированных вопросов."}, nil
	}

	// Sort questions by time
	sort.Slice(questions, func(i, j int) bool {
		return questions[i].SendAt.Before(questions[j].SendAt)
	})

	// Format each question
	var formattedQuestions []string
	for _, q := range questions {
		formatted := fmt.Sprintf("%s %s",
			q.SendAt.Format("02.01 15:04"),
			q.Content)
		formattedQuestions = append(formattedQuestions, formatted)
	}

	// Split into blocks of 15
	const blockSize = 15
	var blocks []string

	for i := 0; i < len(formattedQuestions); i += blockSize {
		end := i + blockSize
		if end > len(formattedQuestions) {
			end = len(formattedQuestions)
		}

		block := strings.Join(formattedQuestions[i:end], "\n")
		if len(formattedQuestions) > blockSize {
			block = fmt.Sprintf("Вопросы %d-%d из %d:\n\n%s",
				i+1, end, len(formattedQuestions), block)
		}
		blocks = append(blocks, block)
	}

	return blocks, nil
}
