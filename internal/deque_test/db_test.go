// db_test.go
package deque_test

import (
	"testing"
	"time"

	"deque/internal/deque"

	"github.com/stretchr/testify/require"
)

func TestDatabaseOperations(t *testing.T) {
	// Setup
	db, ok := deque.LoadDatabase(":memory:") // Use in-memory SQLite for testing
	require.True(t, ok)
	require.NotNil(t, db)

	t.Run("AddAndRetrieveQuestion", func(t *testing.T) {
		// Arrange
		now := time.Now()
		question := deque.Question{
			SendAt:  now,
			Content: "Test question",
		}

		// Act
		question, err := db.AddQuestion(question)
		require.NoError(t, err)

		// Get the question (assuming ID 1 since it's the first entry)
		retrieved, err := db.GetQuestionByID(1)

		// require
		require.NoError(t, err)
		require.Equal(t, question.Content, retrieved.Content)
		require.WithinDuration(t, question.SendAt, retrieved.SendAt, time.Second)
	})

	t.Run("GetNonExistentQuestion", func(t *testing.T) {
		// Act
		_, err := db.GetQuestionByID(9999)

		// require
		require.ErrorIs(t, err, deque.ErrNotFound)
	})

	t.Run("LoadFutureQuestions", func(t *testing.T) {
		// Arrange
		futureTime := time.Now().Add(24 * time.Hour)
		futureQuestion := deque.Question{
			SendAt:  futureTime,
			Content: "Future question",
		}
		futureQuestion, err := db.AddQuestion(futureQuestion)
		require.NoError(t, err)

		// Act
		questions := db.LoadFutureQuestions()

		// require
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
	})

	t.Run("NextEmptyDate", func(t *testing.T) {
		// Act
		nextDate := db.NextEmptyDate()

		// require
		require.True(t, nextDate.After(time.Now()))
		// Should be at 9:00
		require.Equal(t, 9, nextDate.Hour())
		require.Equal(t, 0, nextDate.Minute())
	})

	t.Run("LoadStats", func(t *testing.T) {
		// Act
		stats, err := db.LoadStats()

		// require
		require.NoError(t, err)
		require.Greater(t, stats.TotalQuestions, int64(0))
		require.Greater(t, stats.FutureQuestions, int64(0))
		require.NotEmpty(t, stats.UpcomingQuestion)
	})
}

func TestDatabaseLoadFailure(t *testing.T) {
	// Test with invalid path
	db, ok := deque.LoadDatabase("/invalid/path/that/doesnt/exist/database.db")
	require.False(t, ok)
	require.Nil(t, db)
}

func TestAddQuestionWithInvalidData(t *testing.T) {
	// Setup
	db, ok := deque.LoadDatabase(":memory:")
	require.True(t, ok)

	// Test with zero time
	question := deque.Question{
		SendAt:  time.Time{}, // Zero time
		Content: "",          // Empty content
	}
	question, err := db.AddQuestion(question)
	require.NoError(t, err) // SQLite should accept this, but you might want to add validation
}
