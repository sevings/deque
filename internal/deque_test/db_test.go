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

	futureTime := time.Now().Add(24 * time.Hour)
	futureQuestion := deque.Question{
		SendAt:  futureTime,
		Content: "Future question",
	}
	futureQuestion, err := db.AddQuestion(futureQuestion)
	require.NoError(t, err)

	questions := db.LoadFutureQuestions()

	require.NotEmpty(t, questions)
	require.Greater(t, len(questions), 0)
	found := false
	for _, q := range questions {
		if q.Content == "Future question" {
			found = true
			require.WithinDuration(t, futureTime, q.SendAt, time.Second)
		}
	}
	require.True(t, found)
}

func TestNextEmptyDate(t *testing.T) {
	db, ok := deque.LoadDatabase(":memory:")
	require.True(t, ok)
	require.NotNil(t, db)

	nextDate := db.NextEmptyDate()

	require.True(t, nextDate.After(time.Now()))
	require.Equal(t, 9, nextDate.Hour())
	require.Equal(t, 0, nextDate.Minute())
}

func TestLoadStats(t *testing.T) {
	db, ok := deque.LoadDatabase(":memory:")
	require.True(t, ok)
	require.NotNil(t, db)

	futureTime := time.Now().Add(24 * time.Hour)
	question := deque.Question{
		SendAt:  futureTime,
		Content: "Stats test question",
	}
	_, err := db.AddQuestion(question)
	require.NoError(t, err)

	stats, err := db.LoadStats()

	require.NoError(t, err)
	require.Greater(t, stats.TotalQuestions, int64(0))
	require.Greater(t, stats.FutureQuestions, int64(0))
	require.NotEmpty(t, stats.UpcomingQuestion)
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
