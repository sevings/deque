// src/deque/internal/deque/deque_test.go
package deque_test

import (
	"testing"
	"time"

	"deque/internal/deque"

	"github.com/stretchr/testify/require"
)

type MockScheduler struct {
	jobFunc  deque.JobFunc
	jobs     map[deque.JobID]time.Time
	executed map[deque.JobID]bool
}

func NewMockScheduler() *MockScheduler {
	return &MockScheduler{
		jobs:     make(map[deque.JobID]time.Time),
		executed: make(map[deque.JobID]bool),
	}
}

func (s *MockScheduler) SetJobFunc(fn deque.JobFunc) {
	s.jobFunc = fn
}

func (s *MockScheduler) Schedule(at time.Time, id deque.JobID) {
	s.jobs[id] = at
}

func (s *MockScheduler) Cancel(id deque.JobID) {
	delete(s.jobs, id)
}

// Additional methods for testing
func (s *MockScheduler) ExecuteJob(id deque.JobID) {
	if s.jobFunc != nil {
		s.jobFunc(id)
		s.executed[id] = true
	}
}

func (s *MockScheduler) IsScheduled(id deque.JobID) bool {
	_, exists := s.jobs[id]
	return exists
}

func (s *MockScheduler) WasExecuted(id deque.JobID) bool {
	return s.executed[id]
}

func setupTest(t *testing.T) (*deque.Deque, *deque.DB, *MockScheduler) {
	db, ok := deque.LoadDatabase(":memory:")
	require.True(t, ok)

	cfg := deque.Config{
		DefaultTime: "09:00",
	}

	sched := NewMockScheduler()
	d := deque.NewDeque(db, cfg, sched)

	return d, db, sched
}

func TestDeque_ScheduleQuestions(t *testing.T) {
	d, db, sched := setupTest(t)

	d.SetAskFunc(func(q deque.Question) {})

	// Test scheduling with different formats
	input := `
		25.12 Hello World
		15:00 Afternoon message
		Simple message
		25.12 10:00 Christmas morning
	`

	err := d.ScheduleQuestions(input)
	require.NoError(t, err)

	// Load stats to verify questions were added
	stats, err := db.LoadStats()
	require.NoError(t, err)
	require.Equal(t, int64(4), stats.TotalQuestions)

	// Verify future questions
	questions := db.LoadFutureQuestions()
	require.Len(t, questions, 4)

	require.Len(t, sched.jobs, 4)

	// Verify scheduler was called for each question
	for _, q := range questions {
		require.True(t, sched.IsScheduled(deque.JobID(q.ID)))
	}
}

func TestDeque_AskFunction(t *testing.T) {
	d, db, sched := setupTest(t)

	var askedQuestion deque.Question
	d.SetAskFunc(func(q deque.Question) {
		askedQuestion = q
	})

	// Schedule a question
	err := d.ScheduleQuestions("Test question")
	require.NoError(t, err)

	// Get the scheduled question
	questions := db.LoadFutureQuestions()
	require.Len(t, questions, 1)

	// Execute the scheduled job
	sched.ExecuteJob(deque.JobID(questions[0].ID))

	// Verify the question was asked
	require.Equal(t, questions[0].Content, askedQuestion.Content)
	require.True(t, sched.WasExecuted(deque.JobID(questions[0].ID)))
}

func TestDeque_ScheduleWithInvalidDate(t *testing.T) {
	d, _, _ := setupTest(t)
	d.SetAskFunc(func(q deque.Question) {})

	// Test with invalid date format
	err := d.ScheduleQuestions("13.40 Invalid date")
	require.Error(t, err)
}

func TestDeque_NextEmptyDate(t *testing.T) {
	d, db, _ := setupTest(t)
	d.SetAskFunc(func(q deque.Question) {})

	// Schedule a question for tomorrow
	tomorrow := time.Now().AddDate(0, 0, 1)
	tomorrow = time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 9, 0, 0, 0, time.Local)

	err := d.ScheduleQuestions(tomorrow.Format("02.01") + " Test question")
	require.NoError(t, err)

	// Get next empty date
	nextEmpty := db.NextEmptyDate()

	// Should be day after tomorrow
	expected := tomorrow.AddDate(0, 0, 1)
	require.Equal(t, expected.Year(), nextEmpty.Year())
	require.Equal(t, expected.Month(), nextEmpty.Month())
	require.Equal(t, expected.Day(), nextEmpty.Day())
}

func TestDeque_MultipleQuestionsOnSameDay(t *testing.T) {
	d, db, _ := setupTest(t)
	d.SetAskFunc(func(q deque.Question) {})

	input := `
		25.12 09:00 Morning
		25.12 15:00 Afternoon
		25.12 21:00 Evening
	`

	err := d.ScheduleQuestions(input)
	require.NoError(t, err)

	questions := db.LoadFutureQuestions()
	require.Len(t, questions, 3)

	// Verify different times on the same day
	times := make(map[time.Time]bool)
	for _, q := range questions {
		times[q.SendAt] = true
	}
	require.Len(t, times, 3) // Should have 3 different times
}

func TestDeque_Start(t *testing.T) {
	d, db, sched := setupTest(t)

	// Try to start without setting ask function
	err := d.Start()
	require.Error(t, err)
	require.Contains(t, err.Error(), "ask function not set")

	// Set ask function
	d.SetAskFunc(func(q deque.Question) {})

	// Schedule some questions
	input := `
        25.12 First question
        26.12 Second question
        27.12 Third question
    `
	err = d.ScheduleQuestions(input)
	require.NoError(t, err)

	// Clear scheduler's jobs
	sched.jobs = make(map[deque.JobID]time.Time)

	// Start deque
	err = d.Start()
	require.NoError(t, err)

	// Verify all future questions were scheduled
	questions := db.LoadFutureQuestions()
	require.Len(t, sched.jobs, len(questions))

	for _, q := range questions {
		require.True(t, sched.IsScheduled(deque.JobID(q.ID)))
		require.Equal(t, q.SendAt, sched.jobs[deque.JobID(q.ID)])
	}
}

func TestDeque_StartWithPastQuestions(t *testing.T) {
	d, db, sched := setupTest(t)
	d.SetAskFunc(func(q deque.Question) {})

	now := time.Now()

	// Add a question in the past
	pastQuestion := deque.Question{
		SendAt: time.Date(
			now.Year(), now.Month(), now.Day()-1,
			9, 0, 0, 0, time.Local), // Use time.Local
		Content: "Past question",
	}
	_, err := db.AddQuestion(pastQuestion)
	require.NoError(t, err)

	// Add a future question
	futureQuestion := deque.Question{
		SendAt: time.Date(
			now.Year(), now.Month(), now.Day()+1,
			9, 0, 0, 0, time.Local), // Use time.Local
		Content: "Future question",
	}
	saved, err := db.AddQuestion(futureQuestion)
	require.NoError(t, err)

	// Start deque
	err = d.Start()
	require.NoError(t, err)

	// Verify only future question was scheduled
	require.Len(t, sched.jobs, 1)
	require.True(t, sched.IsScheduled(deque.JobID(saved.ID)))

	// Compare times using Equal instead of direct comparison
	require.True(t, saved.SendAt.Equal(sched.jobs[deque.JobID(saved.ID)]))
}

func TestDeque_StartWithNoQuestions(t *testing.T) {
	d, _, sched := setupTest(t)
	d.SetAskFunc(func(q deque.Question) {})

	// Start deque with no questions
	err := d.Start()
	require.NoError(t, err)

	// Verify no jobs were scheduled
	require.Len(t, sched.jobs, 0)
}
