package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/croz-ltd/periscope/internal/store"
)

// GET /api/timeline?key=<key>&key=<key>&days=<1|2|5|7|14|30>[&at=<RFC3339>]
//
// The window is one of a fixed set, because the step has to be chosen with it: a
// day of history at daily resolution is one point, and a month at hourly
// resolution is 720 points per cluster of a value that moves once a week. Each
// timeframe below lands on about 25 to 30 points, which is what a line chart the
// width of a card can show.

type timeframe struct {
	days int
	step time.Duration
}

var timeframes = map[int]timeframe{
	1:  {1, time.Hour},
	2:  {2, 2 * time.Hour},
	5:  {5, 4 * time.Hour},
	7:  {7, 6 * time.Hour},
	14: {14, 12 * time.Hour},
	30: {30, 24 * time.Hour},
}

// allowedDays is the fixed set, in order, for the error message and the UI.
var allowedDays = []int{1, 2, 5, 7, 14, 30}

type timelineResponse struct {
	From  time.Time           `json:"from"`
	To    time.Time           `json:"to"`
	Days  int                 `json:"days"`
	Step  string              `json:"step"` // Go duration, for the axis label
	Rows  []store.TimelineRow `json:"rows"`
	Stale bool                `json:"stale,omitempty"` // set when the window reaches past the oldest snapshot
}

func (s *Server) handleTimeline(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	days := 7
	if raw := q.Get("days"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || timeframes[parsed].days == 0 {
			http.Error(w, fmt.Sprintf("days must be one of %s", joinInts(allowedDays)), http.StatusBadRequest)
			return
		}
		days = parsed
	}
	tf := timeframes[days]

	// Time travel bounds the window's end, so a timeline read while looking at
	// history ends where that history was, not at now.
	to := time.Now().UTC()
	if raw := q.Get("at"); raw != "" {
		at, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			http.Error(w, "at must be an RFC3339 timestamp", http.StatusBadRequest)
			return
		}
		to = at.UTC()
	}
	from := to.Add(-time.Duration(tf.days) * 24 * time.Hour)

	keys := requestedKeys(q["key"])
	if len(keys) == 0 {
		http.Error(w, "at least one key is required", http.StatusBadRequest)
		return
	}
	if len(keys) > maxTimelineKeys {
		http.Error(w, fmt.Sprintf("at most %d keys per request", maxTimelineKeys), http.StatusBadRequest)
		return
	}

	rows, err := s.Store.Timeline(keys, from, to, tf.step)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := timelineResponse{From: from, To: to, Days: days, Step: tf.step.String(), Rows: rows}
	if first, _, err := s.Store.Span(); err == nil && !first.IsZero() && first.After(from) {
		// The fleet has less history than the window asks for. The lines are still
		// true, they just start late, and the UI says so rather than implying the
		// fleet was empty until then.
		resp.Stale = true
	}
	if resp.Rows == nil {
		resp.Rows = []store.TimelineRow{}
	}
	writeJSON(w, resp)
}

// maxTimelineKeys bounds one request. The Statistics page asks for every
// countable row at once, which is a dozen on a large fleet.
const maxTimelineKeys = 40

// requestedKeys accepts both repeated key parameters and comma-separated lists,
// because a URL built by hand tends to use the second form.
func requestedKeys(raw []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, group := range raw {
		for _, key := range strings.Split(group, ",") {
			key = strings.TrimSpace(key)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, key)
		}
	}
	return out
}

func joinInts(values []int) string {
	parts := make([]string, 0, len(values))
	for _, v := range values {
		parts = append(parts, strconv.Itoa(v))
	}
	return strings.Join(parts, ", ")
}
