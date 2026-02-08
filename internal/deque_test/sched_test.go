package deque_test

import (
	"sync"
	"testing"
	"time"

	"deque/internal/deque"

	"github.com/stretchr/testify/require"
)

func TestNewSched(t *testing.T) {
	r := require.New(t)
	s := deque.NewSched(10)
	r.NotNil(s, "NewSched should not return nil")
}

func TestScheduleAndRun(t *testing.T) {
	r := require.New(t)
	s := deque.NewSched(10)

	var wg sync.WaitGroup
	wg.Add(1)

	executed := false
	s.SetJobFunc(func(id deque.JobID) {
		r.Equal(deque.JobID(1), id, "Job ID should be 1")
		executed = true
		wg.Done()
	})

	s.Start()
	s.Schedule(time.Now().Add(50*time.Millisecond), deque.JobID(1))

	wg.Wait()
	s.Stop()

	r.True(executed, "Job should have been executed")
}

func TestCancel(t *testing.T) {
	r := require.New(t)
	s := deque.NewSched(10)

	executed := false
	s.SetJobFunc(func(id deque.JobID) {
		executed = true
	})

	s.Start()
	s.Schedule(time.Now().Add(100*time.Millisecond), deque.JobID(1))
	s.Cancel(deque.JobID(1))

	time.Sleep(200 * time.Millisecond)
	s.Stop()

	r.False(executed, "Job should not have been executed after cancellation")
}

func TestMultipleJobs(t *testing.T) {
	r := require.New(t)
	s := deque.NewSched(10)

	var mu sync.Mutex
	executed := make(map[deque.JobID]bool)

	s.SetJobFunc(func(id deque.JobID) {
		mu.Lock()
		executed[id] = true
		mu.Unlock()
	})

	s.Start()
	s.Schedule(time.Now().Add(50*time.Millisecond), deque.JobID(1))
	s.Schedule(time.Now().Add(60*time.Millisecond), deque.JobID(2))
	s.Schedule(time.Now().Add(70*time.Millisecond), deque.JobID(3))

	time.Sleep(100 * time.Millisecond)
	s.Stop()

	r.Len(executed, 3, "All three jobs should have been executed")
	r.True(executed[deque.JobID(1)], "Job 1 should have been executed")
	r.True(executed[deque.JobID(2)], "Job 2 should have been executed")
	r.True(executed[deque.JobID(3)], "Job 3 should have been executed")
}

func TestConcurrency(t *testing.T) {
	r := require.New(t)
	s := deque.NewSched(100)

	var mu sync.Mutex
	executedCount := 0

	s.SetJobFunc(func(id deque.JobID) {
		mu.Lock()
		executedCount++
		mu.Unlock()
	})

	s.Start()

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(id deque.JobID) {
			defer wg.Done()
			s.Schedule(time.Now().Add(50*time.Millisecond), id)
			if id%2 == 0 {
				time.Sleep(10 * time.Millisecond)
				s.Cancel(id)
			}
		}(deque.JobID(i))
	}

	wg.Wait()
	time.Sleep(100 * time.Millisecond)
	s.Stop()

	r.Less(executedCount, 100, "Some jobs should have been cancelled")
	r.Greater(executedCount, 0, "Some jobs should have been executed")
}

func TestNearestJobExecution(t *testing.T) {
	r := require.New(t)
	s := deque.NewSched(10)

	var mu sync.Mutex
	executed := make([]deque.JobID, 0)

	s.SetJobFunc(func(id deque.JobID) {
		mu.Lock()
		executed = append(executed, id)
		mu.Unlock()
	})

	s.Start()

	// Schedule jobs in non-chronological order
	s.Schedule(time.Now().Add(100*time.Millisecond), deque.JobID(3))
	s.Schedule(time.Now().Add(50*time.Millisecond), deque.JobID(2))
	s.Schedule(time.Now().Add(25*time.Millisecond), deque.JobID(1))

	time.Sleep(150 * time.Millisecond)
	s.Stop()

	r.Equal([]deque.JobID{1, 2, 3}, executed, "Jobs should execute in chronological order")
}

func TestMultipleJobsSameTime(t *testing.T) {
	r := require.New(t)
	s := deque.NewSched(10)

	var mu sync.Mutex
	executed := make([]deque.JobID, 0)

	s.SetJobFunc(func(id deque.JobID) {
		mu.Lock()
		executed = append(executed, id)
		mu.Unlock()
	})

	s.Start()

	// Schedule multiple jobs for the same time
	executionTime := time.Now().Add(50 * time.Millisecond)
	s.Schedule(executionTime, deque.JobID(1))
	s.Schedule(executionTime, deque.JobID(2))
	s.Schedule(executionTime, deque.JobID(3))

	// Schedule one job for later
	s.Schedule(time.Now().Add(100*time.Millisecond), deque.JobID(4))

	time.Sleep(150 * time.Millisecond)
	s.Stop()

	r.Len(executed, 4, "All jobs should have executed")

	// First three jobs should be among IDs 1,2,3 (order not guaranteed)
	firstThree := executed[:3]
	r.Contains(firstThree, deque.JobID(1))
	r.Contains(firstThree, deque.JobID(2))
	r.Contains(firstThree, deque.JobID(3))

	// Last job should be ID 4
	r.Equal(deque.JobID(4), executed[3])
}
