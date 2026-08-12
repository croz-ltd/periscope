package store

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Reading one component's history back as a series, for the timeline charts.
//
// The matrix answers "what is the value now". A snapshot every ten minutes also
// records what it was every ten minutes before that, and a count is the kind of
// value where the shape over a fortnight says more than today's number: nodes
// added, virtual machines piling up, volumes growing.
//
// Values are carried forward. A scrape that found no change writes the same
// value again, and a component that nobody touched for a week still has a value
// on every day of that week, which is why each window is seeded with the last
// value recorded before it starts.

// TimelinePoint is one value at one moment.
type TimelinePoint struct {
	Time    time.Time         `json:"t"`
	Version string            `json:"version"`
	Extra   map[string]string `json:"extra,omitempty"`
}

// TimelineSeries is one cluster's values across the window.
type TimelineSeries struct {
	Cluster string          `json:"cluster"`
	Points  []TimelinePoint `json:"points"`
}

// TimelineRow is one component across every cluster that reported it.
type TimelineRow struct {
	Key    string           `json:"key"`
	Name   string           `json:"name"`
	Series []TimelineSeries `json:"series"`
}

// value is the state carried between snapshots while walking one cluster.
type value struct {
	version string
	extra   map[string]string
	ok      bool
}

// Timeline returns one row per key, holding each cluster's value at every step
// boundary in (from, to]. A cluster appears only from the first boundary where it
// had a value, so a cluster joined last Tuesday does not draw a flat line back
// through the whole window.
func (s *Store) Timeline(keys []string, from, to time.Time, step time.Duration) ([]TimelineRow, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	if step <= 0 {
		return nil, fmt.Errorf("timeline step must be positive, got %s", step)
	}
	if !to.After(from) {
		return nil, fmt.Errorf("timeline window ends before it starts: %s to %s", from, to)
	}

	boundaries := stepBoundaries(from, to, step)
	if len(boundaries) == 0 {
		return nil, nil
	}

	// One pass, ordered by cluster then time, so each cluster's values are walked
	// forward once. The seed CTE finds the last snapshot before the window per
	// cluster, because the value at the first boundary was usually recorded before
	// the window opened.
	placeholders := strings.Repeat("?,", len(keys)-1) + "?"
	args := make([]any, 0, len(keys)*2+3)
	for _, k := range keys {
		args = append(args, k)
	}
	args = append(args, from.Unix())
	for _, k := range keys {
		args = append(args, k)
	}
	args = append(args, from.Unix(), to.Unix())

	rows, err := s.db.Query(`
WITH seed AS (
  SELECT s.cluster AS cluster, c.comp_key AS comp_key, MAX(s.ts) AS ts
  FROM snapshots s
  JOIN components c ON c.snapshot_id = s.id
  WHERE c.comp_key IN (`+placeholders+`) AND s.ok = 1 AND s.ts <= ?
  GROUP BY s.cluster, c.comp_key
)
SELECT s.cluster, s.ts, c.comp_key, COALESCE(c.name, ''), COALESCE(c.version, ''), COALESCE(c.extra, '')
FROM snapshots s
JOIN components c ON c.snapshot_id = s.id
WHERE c.comp_key IN (`+placeholders+`) AND s.ok = 1
  AND ((s.ts > ? AND s.ts <= ?)
       OR EXISTS (SELECT 1 FROM seed
                  WHERE seed.cluster = s.cluster AND seed.comp_key = c.comp_key AND seed.ts = s.ts))
ORDER BY s.cluster, s.ts, s.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// series[key][cluster] accumulates while the rows are walked.
	series := map[string]map[string][]TimelinePoint{}
	names := map[string]string{}
	for _, k := range keys {
		series[k] = map[string][]TimelinePoint{}
	}

	var (
		cluster string
		started bool
		current = map[string]value{} // per key, the latest value seen for this cluster
		next    = map[string]int{}   // per key, the boundary still to be filled
	)

	// emit fills every boundary up to ts with the value carried into it.
	emit := func(upTo time.Time) {
		for key, val := range current {
			if !val.ok {
				continue
			}
			for next[key] < len(boundaries) && !boundaries[next[key]].After(upTo) {
				at := boundaries[next[key]]
				series[key][cluster] = append(series[key][cluster], TimelinePoint{
					Time: at, Version: val.version, Extra: val.extra,
				})
				next[key]++
			}
		}
	}

	// finish tops up the boundaries after the last snapshot of a cluster, because
	// the value it was last seen with is the value it still has.
	finish := func() {
		if started {
			emit(to)
		}
	}

	for rows.Next() {
		var (
			rowCluster, key, name, ver, extraJSON string
			ts                                    int64
		)
		if err := rows.Scan(&rowCluster, &ts, &key, &name, &ver, &extraJSON); err != nil {
			return nil, err
		}
		if !started || rowCluster != cluster {
			finish()
			cluster, started = rowCluster, true
			current = map[string]value{}
			next = map[string]int{}
		}
		at := time.Unix(ts, 0).UTC()
		// Boundaries strictly before this snapshot still carry the previous value.
		emit(at.Add(-time.Nanosecond))
		if _, had := current[key]; !had {
			// The first value this cluster reported for this component. Boundaries
			// before it are not zero and not this value either: the cluster had not
			// reported the component yet, so the line starts here.
			for next[key] < len(boundaries) && boundaries[next[key]].Before(at) {
				next[key]++
			}
		}
		if name != "" {
			names[key] = name
		}
		current[key] = value{version: ver, extra: decodeExtra(extraJSON), ok: true}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	finish()

	out := make([]TimelineRow, 0, len(keys))
	for _, key := range keys {
		row := TimelineRow{Key: key, Name: names[key]}
		if row.Name == "" {
			row.Name = key
		}
		for cl, points := range series[key] {
			if len(points) > 0 {
				row.Series = append(row.Series, TimelineSeries{Cluster: cl, Points: points})
			}
		}
		sort.Slice(row.Series, func(i, j int) bool { return row.Series[i].Cluster < row.Series[j].Cluster })
		out = append(out, row)
	}
	return out, nil
}

// stepBoundaries lists the moments a series reports, oldest first, ending at to
// so the last point is the current value rather than an arbitrary step short of
// it.
func stepBoundaries(from, to time.Time, step time.Duration) []time.Time {
	var out []time.Time
	for at := to; at.After(from); at = at.Add(-step) {
		out = append(out, at.UTC())
	}
	// Built backwards from `to`, so reverse into time order.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// decodeExtra reads the stored JSON blob, tolerating an empty or broken one: the
// extra map is detail on top of the value, never the value itself.
func decodeExtra(blob string) map[string]string {
	if blob == "" || blob == "null" {
		return nil
	}
	var extra map[string]string
	if err := json.Unmarshal([]byte(blob), &extra); err != nil {
		return nil
	}
	return extra
}
