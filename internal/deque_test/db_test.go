package deque_test

import (
	"testing"
	"time"

	"deque/internal/deque"

	"github.com/stretchr/testify/require"
)

func TestAddAndRetrieveQuestion(t *testing.T) {
	db, ok := deque.LoadDatabase(":memory:") // Use in-memory SQLite for testing
	require.True(t, ok)
	require.NotNil(t, db)

	now := time.Now()
	question := deque.Question{
		SendAt:  now,
		Content: "Test question",
	}

	question, err := db.AddQuestion(question)
	require.NoError(t, err)

	retrieved, err := db.GetQuestionByID(question.ID)
	require.NoError(t, err)
	require.Equal(t, question.Content, retrieved.Content)
	require.WithinDuration(t, question.SendAt, retrieved.SendAt, time.Second)
}

func TestGetNonExistentQuestion(t *testing.T) {
	db, ok := deque.LoadDatabase(":memory:")
	require.True(t, ok)
	require.NotNil(t, db)

	_, err := db.GetQuestionByID(9999)
	require.ErrorIs(t, err, deque.ErrNotFound)
}

func TestLoadFutureQuestions(t *testing.T) {
	db, ok := deque.LoadDatabase(":memory:")
	require.True(t, ok)
	require.NotNil(t, db)

	pastTime := time.Now().Add(-24 * time.Hour)
	pastQuestion := deque.Question{
		SendAt:  pastTime,
		Content: "Past question",
	}
	_, err := db.AddQuestion(pastQuestion)
	require.NoError(t, err)

	futureTime1 := time.Now().Add(24 * time.Hour)
	futureQuestion1 := deque.Question{
		SendAt:  futureTime1,
		Content: "Future question 1",
	}
	_, err = db.AddQuestion(futureQuestion1)
	require.NoError(t, err)

	futureTime2 := time.Now().Add(48 * time.Hour)
	futureQuestion2 := deque.Question{
		SendAt:  futureTime2,
		Content: "Future question 2",
	}
	_, err = db.AddQuestion(futureQuestion2)
	require.NoError(t, err)

	questions := db.LoadFutureQuestions()
	require.Equal(t, 2, len(questions))

	for _, q := range questions {
		require.True(t, q.SendAt.After(time.Now()), "Only future questions should be returned")
		require.NotEqual(t, pastQuestion.ID, q.ID, "Past question should not be included")
	}

	var foundQ1, foundQ2 bool
	for _, q := range questions {
		if q.Content == "Future question 1" {
			foundQ1 = true
			require.WithinDuration(t, futureTime1, q.SendAt, time.Second)
		}
		if q.Content == "Future question 2" {
			foundQ2 = true
			require.WithinDuration(t, futureTime2, q.SendAt, time.Second)
		}
	}
	require.True(t, foundQ1, "Future question 1 should be included")
	require.True(t, foundQ2, "Future question 2 should be included")
}

func TestNextEmptyDate(t *testing.T) {
	db, ok := deque.LoadDatabase(":memory:")
	require.True(t, ok)
	require.NotNil(t, db)

	now := time.Now()
	loc := now.Location()

	// Test 1: Check if it returns today when time is in future
	// Set scheduled time to be 2 hours in the future
	futureHour := (now.Hour() + 2) % 24
	futureMinute := 0

	nextDate := db.NextEmptyDate(futureHour, futureMinute, loc)
	require.Equal(t, now.Year(), nextDate.Year())
	require.Equal(t, now.Month(), nextDate.Month())
	require.Equal(t, now.Day(), nextDate.Day())
	require.Equal(t, futureHour, nextDate.Hour())
	require.Equal(t, futureMinute, nextDate.Minute())

	// Test 2: Check if it returns tomorrow when time is in past
	pastHour := (now.Hour() + 23) % 24 // Hour from yesterday at this time
	pastMinute := 0

	tomorrow := now.AddDate(0, 0, 1)
	tomorrow = time.Date(
		tomorrow.Year(),
		tomorrow.Month(),
		tomorrow.Day(),
		pastHour, pastMinute, 0, 0,
		tomorrow.Location(),
	)

	nextDate = db.NextEmptyDate(pastHour, pastMinute, loc)
	require.True(t, nextDate.After(now))
	require.Equal(t, pastHour, nextDate.Hour())
	require.Equal(t, pastMinute, nextDate.Minute())
	require.WithinDuration(t, tomorrow, nextDate, time.Second)

	// Test 3: Add a question for today's future time and verify next empty date is tomorrow
	todayFutureTime := time.Date(
		now.Year(),
		now.Month(),
		now.Day(),
		futureHour, futureMinute, 0, 0,
		now.Location(),
	)

	todayQuestion := deque.Question{
		SendAt:  todayFutureTime,
		Content: "Today's question",
	}
	_, err := db.AddQuestion(todayQuestion)
	require.NoError(t, err)

	nextDate = db.NextEmptyDate(futureHour, futureMinute, loc)
	expectedDate := now.AddDate(0, 0, 1)
	expectedDate = time.Date(
		expectedDate.Year(),
		expectedDate.Month(),
		expectedDate.Day(),
		futureHour, futureMinute, 0, 0,
		expectedDate.Location(),
	)
	require.WithinDuration(t, expectedDate, nextDate, time.Second)

	// Test 4: Add a question for tomorrow and verify next empty date is day after tomorrow
	tomorrowQuestion := deque.Question{
		SendAt:  expectedDate,
		Content: "Tomorrow's question",
	}
	_, err = db.AddQuestion(tomorrowQuestion)
	require.NoError(t, err)

	nextDate = db.NextEmptyDate(futureHour, futureMinute, loc)
	dayAfterTomorrow := expectedDate.AddDate(0, 0, 1)
	require.WithinDuration(t, dayAfterTomorrow, nextDate, time.Second)
	require.Equal(t, futureHour, nextDate.Hour())
	require.Equal(t, futureMinute, nextDate.Minute())

	// Test 5: Add questions for multiple consecutive days and verify correct empty date
	dayAfterTomorrowQuestion := deque.Question{
		SendAt:  dayAfterTomorrow,
		Content: "Day after tomorrow's question",
	}
	_, err = db.AddQuestion(dayAfterTomorrowQuestion)
	require.NoError(t, err)

	thirdDayQuestion := deque.Question{
		SendAt:  dayAfterTomorrow.AddDate(0, 0, 1),
		Content: "Third day's question",
	}
	_, err = db.AddQuestion(thirdDayQuestion)
	require.NoError(t, err)

	nextDate = db.NextEmptyDate(futureHour, futureMinute, loc)
	expectedDate = dayAfterTomorrow.AddDate(0, 0, 2) // Fourth day
	require.WithinDuration(t, expectedDate, nextDate, time.Second)
	require.Equal(t, futureHour, nextDate.Hour())
	require.Equal(t, futureMinute, nextDate.Minute())

	// Test 6: Add a question for a day far in the future and verify it doesn't affect the result
	sixthDayQuestion := deque.Question{
		SendAt:  dayAfterTomorrow.AddDate(0, 0, 4),
		Content: "Sixth day's question",
	}
	_, err = db.AddQuestion(sixthDayQuestion)
	require.NoError(t, err)

	nextDate = db.NextEmptyDate(futureHour, futureMinute, loc)
	require.WithinDuration(t, expectedDate, nextDate, time.Second)
	require.Equal(t, futureHour, nextDate.Hour())
	require.Equal(t, futureMinute, nextDate.Minute())
}

func TestLoadStats(t *testing.T) {
	db, ok := deque.LoadDatabase(":memory:")
	require.True(t, ok)
	require.NotNil(t, db)

	// Case 1: Empty database
	emptyStats, err := db.LoadStats()
	require.NoError(t, err)
	require.Equal(t, int64(0), emptyStats.TotalQuestions)
	require.Equal(t, int64(0), emptyStats.FutureQuestions)
	require.Equal(t, int64(0), emptyStats.PastQuestions)
	require.Empty(t, emptyStats.UpcomingQuestion.Content)

	// Case 2: Add multiple past questions
	pastTime1 := time.Now().Add(-48 * time.Hour)
	pastQuestion1 := deque.Question{
		SendAt:  pastTime1,
		Content: "Past question 1",
	}
	_, err = db.AddQuestion(pastQuestion1)
	require.NoError(t, err)

	pastTime2 := time.Now().Add(-24 * time.Hour)
	pastQuestion2 := deque.Question{
		SendAt:  pastTime2,
		Content: "Past question 2",
	}
	_, err = db.AddQuestion(pastQuestion2)
	require.NoError(t, err)

	// Case 3: Add multiple future questions
	futureTime1 := time.Now().Add(24 * time.Hour)
	futureQuestion1 := deque.Question{
		SendAt:  futureTime1,
		Content: "Future question 1",
	}
	_, err = db.AddQuestion(futureQuestion1)
	require.NoError(t, err)

	futureTime2 := time.Now().Add(48 * time.Hour)
	futureQuestion2 := deque.Question{
		SendAt:  futureTime2,
		Content: "Future question 2",
	}
	_, err = db.AddQuestion(futureQuestion2)
	require.NoError(t, err)

	futureTime3 := time.Now().Add(72 * time.Hour)
	futureQuestion3 := deque.Question{
		SendAt:  futureTime3,
		Content: "Future question 3",
	}
	_, err = db.AddQuestion(futureQuestion3)
	require.NoError(t, err)

	// Now check stats with all questions added
	stats, err := db.LoadStats()
	require.NoError(t, err)

	// Total should be sum of past and future
	require.Equal(t, int64(5), stats.TotalQuestions, "Total questions should be 5")
	require.Equal(t, int64(3), stats.FutureQuestions, "Future questions should be 3")
	require.Equal(t, int64(2), stats.PastQuestions, "Past questions should be 2")

	// Verify the upcoming question is the earliest future question
	require.NotEmpty(t, stats.UpcomingQuestion)
	require.Equal(t, "Future question 1", stats.UpcomingQuestion.Content)
	require.WithinDuration(t, futureTime1, stats.UpcomingQuestion.SendAt, time.Second)

	// Case 4: Add a question even closer to the current time
	veryNearFutureTime := time.Now().Add(1 * time.Hour)
	veryNearFutureQuestion := deque.Question{
		SendAt:  veryNearFutureTime,
		Content: "Very near future question",
	}
	_, err = db.AddQuestion(veryNearFutureQuestion)
	require.NoError(t, err)

	// Check that the upcoming question has changed
	updatedStats, err := db.LoadStats()
	require.NoError(t, err)
	require.Equal(t, int64(6), updatedStats.TotalQuestions)
	require.Equal(t, int64(4), updatedStats.FutureQuestions)
	require.Equal(t, int64(2), updatedStats.PastQuestions)

	// The upcoming question should now be the very near future question
	require.Equal(t, "Very near future question", updatedStats.UpcomingQuestion.Content)
	require.WithinDuration(t, veryNearFutureTime, updatedStats.UpcomingQuestion.SendAt, time.Second)
}

func TestDatabaseLoadFailure(t *testing.T) {
	db, ok := deque.LoadDatabase("/invalid/path/that/doesnt/exist/database.db")
	require.False(t, ok)
	require.Nil(t, db)
}

func TestAddQuestionWithInvalidData(t *testing.T) {
	db, ok := deque.LoadDatabase(":memory:")
	require.True(t, ok)

	question := deque.Question{
		SendAt:  time.Time{},
		Content: "",
	}
	question, err := db.AddQuestion(question)
	require.NoError(t, err)
}
