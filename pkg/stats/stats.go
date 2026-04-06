package stats

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

type Counter struct {
	mu       sync.Mutex
	Total    int64
	ByStatus map[string]int64
}

func newCounter() *Counter {
	return &Counter{ByStatus: make(map[string]int64)}
}

func (c *Counter) inc(status string) {
	c.mu.Lock()
	c.Total++
	c.ByStatus[status]++
	c.mu.Unlock()
}

type Stats struct {
	Service   string
	Version   string
	StartedAt time.Time
	counter   *Counter
}

func NewStats(service, version string) *Stats {
	return &Stats{
		Service:   service,
		Version:   version,
		StartedAt: time.Now(),
		counter:   newCounter(),
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func Middleware(s *Stats, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.counter.inc(fmt.Sprintf("%d", rec.status))
	})
}

func Handler(s *Stats) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.counter.mu.Lock()
		byStatus := make(map[string]int64, len(s.counter.ByStatus))
		for k, v := range s.counter.ByStatus {
			byStatus[k] = v
		}
		total := s.counter.Total
		s.counter.mu.Unlock()

		resp := map[string]any{
			"service":         s.Service,
			"version":         s.Version,
			"started_at":      s.StartedAt.UTC().Format(time.RFC3339),
			"uptime_seconds":  int64(time.Since(s.StartedAt).Seconds()),
			"requests": map[string]any{
				"total":     total,
				"by_status": byStatus,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}
