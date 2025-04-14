package deque

import (
	"sync"
	"time"
)

type Sched struct {
	fn      JobFunc
	entries map[JobID]*time.Timer
	mu      sync.Mutex
	done    chan struct{}
	jobCh   chan JobID
}

func NewSched(maxScheduled int) *Sched {
	return &Sched{
		fn:      func(JobID) {},
		entries: make(map[JobID]*time.Timer),
		done:    make(chan struct{}),
		jobCh:   make(chan JobID, maxScheduled),
	}
}

func (s *Sched) SetJobFunc(fn JobFunc) {
	s.fn = fn
}

func (s *Sched) Start() {
	go s.run()
}

func (s *Sched) Schedule(at time.Time, id JobID) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Cancel existing timer for this ID if it exists
	if timer, exists := s.entries[id]; exists {
		timer.Stop()
	}

	delay := time.Until(at)
	timer := time.AfterFunc(delay, func() {
		s.jobCh <- id
	})
	s.entries[id] = timer
}

func (s *Sched) Cancel(id JobID) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if timer, exists := s.entries[id]; exists {
		timer.Stop()
		delete(s.entries, id)
	}
}

func (s *Sched) run() {
	for {
		select {
		case <-s.done:
			return
		case id := <-s.jobCh:
			s.mu.Lock()
			delete(s.entries, id)
			s.mu.Unlock()
			s.fn(id)
		}
	}
}

func (s *Sched) Stop() {
	close(s.done)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, timer := range s.entries {
		timer.Stop()
	}
	s.entries = nil
}
