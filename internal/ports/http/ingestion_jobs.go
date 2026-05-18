package httpapi

import (
	"fmt"
	"sync"
	"time"
)

const (
	ingestionStatusQueued  = "queued"
	ingestionStatusRunning = "running"
	ingestionStatusSuccess = "success"
	ingestionStatusPartial = "partial_failure"
	ingestionStatusFailed  = "failed"

	maxStoredIngestionJobs = 20
)

type ingestionJobStore struct {
	mu     sync.Mutex
	jobs   map[string]IngestionJobResponse
	order  []string
	active string
}

func newIngestionJobStore() *ingestionJobStore {
	return &ingestionJobStore{jobs: make(map[string]IngestionJobResponse)}
}

func (s *ingestionJobStore) enqueue(days int, since time.Time) (IngestionJobResponse, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.active != "" {
		return s.jobs[s.active], false
	}

	now := time.Now().UTC()
	job := IngestionJobResponse{
		JobID:    fmt.Sprintf("%d", now.UnixNano()),
		Status:   ingestionStatusQueued,
		QueuedAt: now,
		Days:     days,
		Since:    since,
	}
	s.jobs[job.JobID] = job
	s.order = append(s.order, job.JobID)
	s.active = job.JobID
	s.trimLocked()
	return job, true
}

func (s *ingestionJobStore) markRunning(jobID string) IngestionJobResponse {
	s.mu.Lock()
	defer s.mu.Unlock()

	job := s.jobs[jobID]
	now := time.Now().UTC()
	job.Status = ingestionStatusRunning
	job.StartedAt = &now
	s.jobs[jobID] = job
	return job
}

func (s *ingestionJobStore) complete(jobID, status string, results []IngestResponse, err error) IngestionJobResponse {
	s.mu.Lock()
	defer s.mu.Unlock()

	job := s.jobs[jobID]
	now := time.Now().UTC()
	job.Status = status
	job.CompletedAt = &now
	job.Results = results
	if job.StartedAt != nil {
		job.DurationSeconds = now.Sub(*job.StartedAt).Seconds()
	}
	if err != nil {
		job.Error = err.Error()
	}
	s.jobs[jobID] = job
	if s.active == jobID {
		s.active = ""
	}
	return job
}

func (s *ingestionJobStore) get(jobID string) (IngestionJobResponse, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[jobID]
	return job, ok
}

func (s *ingestionJobStore) trimLocked() {
	for len(s.order) > maxStoredIngestionJobs {
		oldest := s.order[0]
		if oldest == s.active {
			return
		}
		delete(s.jobs, oldest)
		s.order = s.order[1:]
	}
}
