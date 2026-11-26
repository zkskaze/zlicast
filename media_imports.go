package main

import (
	"context"
	"net/http"
	"sync"
	"time"
)

type mediaImport struct {
	User      string    `json:"-"`
	Finished  time.Time `json:"-"`
	Status    string    `json:"status"`
	Duplicate bool      `json:"duplicate"`
	Media     *Media    `json:"media,omitempty"`
	Error     string    `json:"error,omitempty"`
}

type mediaImports struct {
	mu      sync.Mutex
	workers sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
	closed  bool
	pending int
	jobs    map[string]mediaImport
}

func (q *mediaImports) start(user string, run func(context.Context) (Media, bool, error)) (string, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed || q.pending >= 4 {
		return "", false
	}
	if q.jobs == nil {
		q.jobs = make(map[string]mediaImport)
		q.ctx, q.cancel = context.WithCancel(context.Background())
	}
	cutoff := time.Now().Add(-time.Hour)
	for id, job := range q.jobs {
		if !job.Finished.IsZero() && job.Finished.Before(cutoff) {
			delete(q.jobs, id)
		}
	}
	id := randomID()
	q.jobs[id] = mediaImport{User: user, Status: "processing"}
	q.pending++
	q.workers.Add(1)
	go func() {
		defer q.workers.Done()
		media, duplicate, err := run(q.ctx)
		job := mediaImport{User: user, Finished: time.Now(), Status: "complete", Duplicate: duplicate, Media: &media}
		if err != nil {
			job.Status, job.Error, job.Media = "failed", err.Error(), nil
		}
		q.mu.Lock()
		q.jobs[id] = job
		q.pending--
		q.mu.Unlock()
	}()
	return id, true
}

func (q *mediaImports) status(w http.ResponseWriter, r *http.Request, user string) {
	q.mu.Lock()
	job, ok := q.jobs[r.PathValue("job")]
	q.mu.Unlock()
	if !ok || job.User != user {
		fail(w, 404, "Обрада није пронађена. Провери медијатеку пре поновног отпремања.")
		return
	}
	respond(w, 200, job)
}

func (q *mediaImports) stop() {
	q.mu.Lock()
	q.closed = true
	if q.cancel != nil {
		q.cancel()
	}
	q.mu.Unlock()
	q.workers.Wait()
}
