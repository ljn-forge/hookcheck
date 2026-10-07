package hookcheck

import "time"

// Report contains metadata only; bodies, headers, URLs and secrets are excluded.
type Report struct {
	Version    int           `json:"version"`
	Scenario   string        `json:"scenario"`
	Seed       int64         `json:"seed"`
	StartedAt  time.Time     `json:"started_at"`
	DurationMS int64         `json:"duration_ms"`
	Passed     bool          `json:"passed"`
	Attempts   []Attempt     `json:"attempts"`
	Checks     []CheckResult `json:"checks"`
}

type Attempt struct {
	ID         int        `json:"id"`
	Step       string     `json:"step"`
	Event      string     `json:"event"`
	Copy       int        `json:"copy"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	DurationMS int64      `json:"duration_ms"`
	Status     int        `json:"status,omitempty"`
	Outcome    string     `json:"outcome"`
	Message    string     `json:"message,omitempty"`
}

type CheckResult struct {
	Name       string `json:"name"`
	Polls      int    `json:"polls"`
	DurationMS int64  `json:"duration_ms"`
	Status     int    `json:"status,omitempty"`
	Outcome    string `json:"outcome"`
	Message    string `json:"message,omitempty"`
}
