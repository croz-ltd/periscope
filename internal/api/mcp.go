package api

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/croz-ltd/periscope/internal/drift"
	"github.com/croz-ltd/periscope/internal/logging"
	"github.com/croz-ltd/periscope/internal/model"
	"github.com/croz-ltd/periscope/internal/store"
	"github.com/croz-ltd/periscope/pkg/version"
)

// The MCP endpoint (/mcp) gives an AI assistant the same data the UI shows. It
// sits behind the same login and the same read check as the REST API, so a
// token that can read the dashboard can use it and no other token can.
//
// The tools return structured JSON. Sums, minimums and the drift list are
// computed here and not left to the model, because a model that adds up a
// column of numbers it read gets it wrong often enough to matter.

const mcpInstructions = `Periscope collects facts from a fleet of OpenShift clusters: versions of OpenShift and of every OLM operator, configuration values, certificate expiry, and counts (nodes, pods, PVCs, VMs, CPU and memory capacity). It scrapes every cluster on an interval and keeps the history.

Every fact is a component with a stable key, for example "openshift", "node-count" or "vm-vcpu-total". Use list_components to find keys, then get_component, get_cluster or fleet_totals to read values. Values are strings as the cluster reported them.

Comparison kinds: "version" rows are judged against the newest version in the fleet (leader, behind). "match" rows are configuration that is the same on every cluster when all is well (match, mismatch). "expiry" rows hold a certificate expiry date. "info" rows are counts and other values with no judgement, and they are the rows to sum.

Capacity rows: "total" is the sum of node capacity. "used (requested)" is the sum of pod requests, not live usage. "free" is allocatable minus requests. VM vCPUs are allocatable cores of the nodes that can run VMs, times the vCPU allocation ratio of OpenShift Virtualization.

Every read tool accepts "at" (RFC3339) to answer about a past moment.`

// newMCPServer builds the MCP server with every tool.
func (s *Server) newMCPServer() *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "periscope", Title: "Periscope", Version: version.Raw},
		&mcp.ServerOptions{Instructions: mcpInstructions})

	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: ptr(false)}
	mcp.AddTool(srv, &mcp.Tool{Name: "list_clusters", Annotations: readOnly,
		Description: "List the joined clusters: last scrape time, whether the last scrape worked, whether the data is stale, the console banner label and the console URL."},
		s.toolListClusters)
	mcp.AddTool(srv, &mcp.Tool{Name: "list_components", Annotations: readOnly,
		Description: "List the component keys the fleet reports, with name, group, comparison kind and the number of clusters that report each. Filter by text or group to find the key to read."},
		s.toolListComponents)
	mcp.AddTool(srv, &mcp.Tool{Name: "get_component", Annotations: readOnly,
		Description: "Read one component on every cluster (or on the named clusters): the value, the drift state, and the details shown in the UI tooltip."},
		s.toolGetComponent)
	mcp.AddTool(srv, &mcp.Tool{Name: "get_cluster", Annotations: readOnly,
		Description: "Read every component of one cluster, optionally limited to one group (for example Capacity, Operators, OpenShift Virtualization)."},
		s.toolGetCluster)
	mcp.AddTool(srv, &mcp.Tool{Name: "fleet_totals", Annotations: readOnly,
		Description: "Sum numeric components across the fleet: total, minimum, maximum, average and the value per cluster. Use this for any question about totals, for example the vCPUs of all clusters."},
		s.toolFleetTotals)
	mcp.AddTool(srv, &mcp.Tool{Name: "find_drift", Annotations: readOnly,
		Description: "List what is out of line: clusters behind the newest version, configuration that differs from the rest of the fleet, certificates that expire within 120 days, and clusters whose scrape failed or is stale."},
		s.toolFindDrift)
	mcp.AddTool(srv, &mcp.Tool{Name: "get_history", Annotations: readOnly,
		Description: "Read the history of up to 10 components per cluster, over 1, 2, 5, 7, 14 or 30 days."},
		s.toolGetHistory)
	mcp.AddTool(srv, &mcp.Tool{Name: "list_changes", Annotations: readOnly,
		Description: "List what changed in the fleet, newest first: components added, removed or updated, and clusters joined, unreachable or recovered. Counter updates are left out unless asked for."},
		s.toolListChanges)
	mcp.AddTool(srv, &mcp.Tool{Name: "refresh",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: ptr(false), IdempotentHint: true, OpenWorldHint: ptr(false)},
		Description: "Start a scrape of every cluster now. It returns at once. The new data shows in the other tools when the scrape is done, usually within a minute."},
		s.toolRefresh)
	return srv
}

func ptr[T any](v T) *T { return &v }

// mcpHandler serves /mcp. It is stateless: every request stands alone, so a
// restart of the hub does not break a client that is connected.
func (s *Server) mcpHandler() http.Handler {
	srv := s.newMCPServer()
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
}

// --- shared input pieces ---

type atInput struct {
	At string `json:"at,omitempty" jsonschema:"optional RFC3339 time, to answer about that moment instead of now"`
}

func (a atInput) time() (time.Time, error) {
	if a.At == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, a.At)
	if err != nil {
		return time.Time{}, fmt.Errorf("at must be an RFC3339 time, for example 2026-11-01T00:00:00Z")
	}
	return t, nil
}

func (s *Server) matrixAt(ctx context.Context, a atInput) (drift.Matrix, error) {
	at, err := a.time()
	if err != nil {
		return drift.Matrix{}, err
	}
	return s.matrix(ctx, at)
}

// clusterFilter matches a cluster by joined name or by banner label, case
// insensitive. An empty list matches every cluster.
func clusterFilter(m drift.Matrix, names []string) (func(string) bool, error) {
	if len(names) == 0 {
		return func(string) bool { return true }, nil
	}
	want := map[string]bool{}
	for _, n := range names {
		found := false
		for _, c := range m.Clusters {
			if strings.EqualFold(c.Name, n) || (c.Label != "" && strings.EqualFold(c.Label, n)) {
				want[c.Name] = true
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("no cluster named %q, list_clusters shows the names", n)
		}
	}
	return func(name string) bool { return want[name] }, nil
}

// sortedClusters returns the cluster names in column order.
func sortedClusters(m drift.Matrix) []string {
	out := make([]string, 0, len(m.Clusters))
	for _, c := range m.Clusters {
		out = append(out, c.Name)
	}
	return out
}

func findRow(m drift.Matrix, key string) (drift.Row, error) {
	for _, r := range m.Rows {
		if r.Key == key {
			return r, nil
		}
	}
	var near []string
	for _, r := range m.Rows {
		if strings.Contains(r.Key, key) || strings.Contains(strings.ToLower(r.Name), strings.ToLower(key)) {
			near = append(near, r.Key)
		}
	}
	sort.Strings(near)
	if len(near) > 10 {
		near = near[:10]
	}
	if len(near) > 0 {
		return drift.Row{}, fmt.Errorf("no component %q. Similar keys: %s", key, strings.Join(near, ", "))
	}
	return drift.Row{}, fmt.Errorf("no component %q, list_components shows the keys", key)
}

// --- list_clusters ---

type clusterOut struct {
	Name       string    `json:"name"`
	Label      string    `json:"label,omitempty"`
	LastScrape time.Time `json:"lastScrape"`
	OK         bool      `json:"ok"`
	Stale      bool      `json:"stale"`
	Error      string    `json:"error,omitempty"`
	Console    string    `json:"console,omitempty"`
}

type listClustersOut struct {
	Clusters []clusterOut `json:"clusters"`
}

func (s *Server) toolListClusters(ctx context.Context, _ *mcp.CallToolRequest, in atInput) (*mcp.CallToolResult, listClustersOut, error) {
	m, err := s.matrixAt(ctx, in)
	if err != nil {
		return nil, listClustersOut{}, err
	}
	out := listClustersOut{Clusters: []clusterOut{}}
	for _, c := range m.Clusters {
		out.Clusters = append(out.Clusters, clusterOut{Name: c.Name, Label: c.Label, LastScrape: c.Time,
			OK: c.OK, Stale: c.Stale, Error: c.Error, Console: c.Console})
	}
	return nil, out, nil
}

// --- list_components ---

type listComponentsIn struct {
	Query string `json:"query,omitempty" jsonschema:"optional text to find in the key or the name, case insensitive"`
	Group string `json:"group,omitempty" jsonschema:"optional group name, for example Capacity or Operators"`
	atInput
}

type componentOut struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	Group     string `json:"group"`
	Compare   string `json:"compare"`
	Kind      string `json:"kind,omitempty"`
	Reporting int    `json:"clustersReporting"`
}

type listComponentsOut struct {
	Components []componentOut `json:"components"`
	Groups     []string       `json:"groups"`
}

func (s *Server) toolListComponents(ctx context.Context, _ *mcp.CallToolRequest, in listComponentsIn) (*mcp.CallToolResult, listComponentsOut, error) {
	m, err := s.matrixAt(ctx, in.atInput)
	if err != nil {
		return nil, listComponentsOut{}, err
	}
	q := strings.ToLower(in.Query)
	groups := map[string]bool{}
	out := listComponentsOut{Components: []componentOut{}}
	for _, r := range m.Rows {
		groups[r.Group] = true
		if in.Group != "" && !strings.EqualFold(r.Group, in.Group) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(r.Key), q) && !strings.Contains(strings.ToLower(r.Name), q) {
			continue
		}
		n := 0
		for _, c := range r.Cells {
			if c.State != drift.StateNotInstalled {
				n++
			}
		}
		out.Components = append(out.Components, componentOut{Key: r.Key, Name: r.Name, Group: r.Group,
			Compare: compareOf(r), Kind: r.Kind, Reporting: n})
	}
	for g := range groups {
		out.Groups = append(out.Groups, g)
	}
	sort.Strings(out.Groups)
	return nil, out, nil
}

func compareOf(r drift.Row) string {
	if r.Compare == "" {
		return model.CompareVersion
	}
	return r.Compare
}

// --- get_component ---

type getComponentIn struct {
	Key      string   `json:"key" jsonschema:"the component key, from list_components"`
	Clusters []string `json:"clusters,omitempty" jsonschema:"optional cluster names or banner labels, all clusters when empty"`
	atInput
}

type cellOut struct {
	Cluster  string            `json:"cluster"`
	Value    string            `json:"value,omitempty"`
	State    string            `json:"state"`
	Gap      string            `json:"gap,omitempty"`
	DaysLeft *int              `json:"daysLeft,omitempty"`
	Details  map[string]string `json:"details,omitempty"`
}

type getComponentOut struct {
	Key     string    `json:"key"`
	Name    string    `json:"name"`
	Group   string    `json:"group"`
	Compare string    `json:"compare"`
	Leader  string    `json:"leader,omitempty"`
	Cells   []cellOut `json:"cells"`
}

func cellOf(r drift.Row, cluster string) cellOut {
	c, ok := r.Cells[cluster]
	if !ok {
		return cellOut{Cluster: cluster, State: string(drift.StateNotInstalled)}
	}
	out := cellOut{Cluster: cluster, Value: c.Version, State: string(c.State), Gap: c.GapKind, Details: c.Extra}
	if strings.HasPrefix(string(c.State), "expiry") {
		out.DaysLeft = ptr(c.Severity)
	}
	return out
}

func (s *Server) toolGetComponent(ctx context.Context, _ *mcp.CallToolRequest, in getComponentIn) (*mcp.CallToolResult, getComponentOut, error) {
	m, err := s.matrixAt(ctx, in.atInput)
	if err != nil {
		return nil, getComponentOut{}, err
	}
	row, err := findRow(m, in.Key)
	if err != nil {
		return nil, getComponentOut{}, err
	}
	keep, err := clusterFilter(m, in.Clusters)
	if err != nil {
		return nil, getComponentOut{}, err
	}
	out := getComponentOut{Key: row.Key, Name: row.Name, Group: row.Group, Compare: compareOf(row), Cells: []cellOut{}}
	if out.Compare != model.CompareInfo && out.Compare != model.CompareExpiry {
		out.Leader = row.Leader
	}
	for _, name := range sortedClusters(m) {
		if keep(name) {
			out.Cells = append(out.Cells, cellOf(row, name))
		}
	}
	return nil, out, nil
}

// --- get_cluster ---

type getClusterIn struct {
	Cluster string `json:"cluster" jsonschema:"the cluster name or its banner label"`
	Group   string `json:"group,omitempty" jsonschema:"optional group, for example Capacity, Operators, Storage"`
	atInput
}

type clusterRowOut struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	Group   string `json:"group"`
	Compare string `json:"compare"`
	cellOut
}

type getClusterOut struct {
	Cluster    clusterOut      `json:"cluster"`
	Components []clusterRowOut `json:"components"`
}

func (s *Server) toolGetCluster(ctx context.Context, _ *mcp.CallToolRequest, in getClusterIn) (*mcp.CallToolResult, getClusterOut, error) {
	m, err := s.matrixAt(ctx, in.atInput)
	if err != nil {
		return nil, getClusterOut{}, err
	}
	keep, err := clusterFilter(m, []string{in.Cluster})
	if err != nil {
		return nil, getClusterOut{}, err
	}
	var out getClusterOut
	for _, c := range m.Clusters {
		if keep(c.Name) {
			out.Cluster = clusterOut{Name: c.Name, Label: c.Label, LastScrape: c.Time, OK: c.OK,
				Stale: c.Stale, Error: c.Error, Console: c.Console}
			break
		}
	}
	out.Components = []clusterRowOut{}
	for _, r := range m.Rows {
		if in.Group != "" && !strings.EqualFold(r.Group, in.Group) {
			continue
		}
		cell := cellOf(r, out.Cluster.Name)
		if cell.State == string(drift.StateNotInstalled) {
			continue
		}
		cell.Cluster = ""
		out.Components = append(out.Components, clusterRowOut{Key: r.Key, Name: r.Name, Group: r.Group,
			Compare: compareOf(r), cellOut: cell})
	}
	sort.Slice(out.Components, func(i, j int) bool {
		a, b := out.Components[i], out.Components[j]
		if a.Group != b.Group {
			return a.Group < b.Group
		}
		return a.Name < b.Name
	})
	return nil, out, nil
}

// --- fleet_totals ---

type fleetTotalsIn struct {
	Keys     []string `json:"keys" jsonschema:"one or more component keys of numeric rows, for example vm-vcpu-total"`
	Clusters []string `json:"clusters,omitempty" jsonschema:"optional cluster names or banner labels, all clusters when empty"`
	atInput
}

type clusterValue struct {
	Cluster string  `json:"cluster"`
	Value   float64 `json:"value"`
}

type totalOut struct {
	Key     string         `json:"key"`
	Name    string         `json:"name"`
	Total   float64        `json:"total"`
	Count   int            `json:"clustersCounted"`
	Average float64        `json:"average"`
	Min     *clusterValue  `json:"min,omitempty"`
	Max     *clusterValue  `json:"max,omitempty"`
	Values  []clusterValue `json:"perCluster"`
	// Pair rows ("12 PVC / 14 PV") are summed per part.
	PVC *float64 `json:"pvcTotal,omitempty"`
	PV  *float64 `json:"pvTotal,omitempty"`
	// NotCounted names the clusters whose value is not a number, and why.
	NotCounted map[string]string `json:"notCounted,omitempty"`
}

type fleetTotalsOut struct {
	Totals []totalOut `json:"totals"`
}

const maxTotalKeys = 50

func (s *Server) toolFleetTotals(ctx context.Context, _ *mcp.CallToolRequest, in fleetTotalsIn) (*mcp.CallToolResult, fleetTotalsOut, error) {
	if len(in.Keys) == 0 {
		return nil, fleetTotalsOut{}, errors.New("name at least one key")
	}
	if len(in.Keys) > maxTotalKeys {
		return nil, fleetTotalsOut{}, fmt.Errorf("%d keys, at most %d per call", len(in.Keys), maxTotalKeys)
	}
	m, err := s.matrixAt(ctx, in.atInput)
	if err != nil {
		return nil, fleetTotalsOut{}, err
	}
	keep, err := clusterFilter(m, in.Clusters)
	if err != nil {
		return nil, fleetTotalsOut{}, err
	}
	out := fleetTotalsOut{Totals: []totalOut{}}
	for _, key := range in.Keys {
		row, err := findRow(m, key)
		if err != nil {
			return nil, fleetTotalsOut{}, err
		}
		out.Totals = append(out.Totals, totalOf(row, sortedClusters(m), keep))
	}
	return nil, out, nil
}

func totalOf(row drift.Row, clusters []string, keep func(string) bool) totalOut {
	t := totalOut{Key: row.Key, Name: row.Name, Values: []clusterValue{}}
	var pvc, pv float64
	pairs := 0
	for _, name := range clusters {
		if !keep(name) {
			continue
		}
		c, ok := row.Cells[name]
		if !ok || c.State == drift.StateNotInstalled {
			continue // absent is not zero
		}
		if v, ok := number(c.Version); ok && compareOf(row) == model.CompareInfo {
			t.Values = append(t.Values, clusterValue{name, v})
			t.Total += v
			if t.Min == nil || v < t.Min.Value {
				t.Min = &clusterValue{name, v}
			}
			if t.Max == nil || v > t.Max.Value {
				t.Max = &clusterValue{name, v}
			}
			continue
		}
		if a, okA := number(c.Extra["pvc"]); okA {
			if b, okB := number(c.Extra["pv"]); okB {
				pvc += a
				pv += b
				pairs++
				continue
			}
		}
		if t.NotCounted == nil {
			t.NotCounted = map[string]string{}
		}
		reason := "not a number: " + c.Version
		if compareOf(row) != model.CompareInfo {
			reason = "a " + compareOf(row) + " row, not a count"
		}
		t.NotCounted[name] = reason
	}
	t.Count = len(t.Values)
	if t.Count > 0 {
		t.Average = math.Round(t.Total/float64(t.Count)*100) / 100
	}
	if pairs > 0 {
		t.PVC, t.PV = &pvc, &pv
		t.Count = pairs
	}
	return t
}

// number reads a whole value as a number. "4.17.12" is not one.
func number(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

// --- find_drift ---

type findDriftIn struct {
	Group    string   `json:"group,omitempty" jsonschema:"optional group to look in, for example Operators"`
	Clusters []string `json:"clusters,omitempty" jsonschema:"optional cluster names or banner labels, all clusters when empty"`
	atInput
}

type driftItem struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Group    string `json:"group"`
	Cluster  string `json:"cluster"`
	Issue    string `json:"issue"` // behind | mismatch | expiry_warn | expiry_crit
	Value    string `json:"value"`
	Expected string `json:"expected,omitempty"`
	Gap      string `json:"gap,omitempty"`
	DaysLeft *int   `json:"daysLeft,omitempty"`
}

type clusterIssue struct {
	Cluster string `json:"cluster"`
	Issue   string `json:"issue"` // scrape_failed | stale
	Detail  string `json:"detail,omitempty"`
}

type findDriftOut struct {
	Clusters []clusterIssue `json:"clusterIssues"`
	Items    []driftItem    `json:"drift"`
	Summary  map[string]int `json:"summary"`
}

func (s *Server) toolFindDrift(ctx context.Context, _ *mcp.CallToolRequest, in findDriftIn) (*mcp.CallToolResult, findDriftOut, error) {
	m, err := s.matrixAt(ctx, in.atInput)
	if err != nil {
		return nil, findDriftOut{}, err
	}
	keep, err := clusterFilter(m, in.Clusters)
	if err != nil {
		return nil, findDriftOut{}, err
	}
	out := findDriftOut{Clusters: []clusterIssue{}, Items: []driftItem{}, Summary: map[string]int{}}
	for _, c := range m.Clusters {
		if !keep(c.Name) {
			continue
		}
		if !c.OK {
			out.Clusters = append(out.Clusters, clusterIssue{c.Name, "scrape_failed", c.Error})
		} else if c.Stale {
			out.Clusters = append(out.Clusters, clusterIssue{c.Name, "stale", "last scrape " + c.Time.UTC().Format(time.RFC3339)})
		}
	}
	for _, r := range m.Rows {
		if in.Group != "" && !strings.EqualFold(r.Group, in.Group) {
			continue
		}
		for _, name := range sortedClusters(m) {
			c, ok := r.Cells[name]
			if !ok || !keep(name) {
				continue
			}
			item := driftItem{Key: r.Key, Name: r.Name, Group: r.Group, Cluster: name, Value: c.Version}
			switch c.State {
			case drift.StateBehind:
				item.Issue, item.Expected, item.Gap = "behind", r.Leader, c.GapKind
			case drift.StateMismatch:
				item.Issue, item.Expected = "mismatch", r.Leader
			case drift.StateExpiryWarn, drift.StateExpiryCrit:
				item.Issue, item.DaysLeft = string(c.State), ptr(c.Severity)
			default:
				continue
			}
			out.Items = append(out.Items, item)
			out.Summary[item.Issue]++
		}
	}
	return nil, out, nil
}

// --- get_history ---

type getHistoryIn struct {
	Keys []string `json:"keys" jsonschema:"one to 10 component keys"`
	Days int      `json:"days,omitempty" jsonschema:"window in days: 1, 2, 5, 7, 14 or 30 (default 7)"`
	atInput
}

type getHistoryOut struct {
	From time.Time           `json:"from"`
	To   time.Time           `json:"to"`
	Step string              `json:"step"`
	Rows []store.TimelineRow `json:"rows"`
}

const maxHistoryKeys = 10

func (s *Server) toolGetHistory(ctx context.Context, _ *mcp.CallToolRequest, in getHistoryIn) (*mcp.CallToolResult, getHistoryOut, error) {
	keys := requestedKeys(in.Keys)
	if len(keys) == 0 || len(keys) > maxHistoryKeys {
		return nil, getHistoryOut{}, fmt.Errorf("name 1 to %d keys", maxHistoryKeys)
	}
	if in.Days == 0 {
		in.Days = 7
	}
	tf, ok := timeframes[in.Days]
	if !ok {
		return nil, getHistoryOut{}, fmt.Errorf("days must be one of %s", joinInts(allowedDays))
	}
	at, err := in.time()
	if err != nil {
		return nil, getHistoryOut{}, err
	}
	to := time.Now().UTC()
	if !at.IsZero() {
		to = at.UTC()
	}
	from := to.Add(-time.Duration(tf.days) * 24 * time.Hour)
	rows, err := s.Store.Timeline(keys, from, to, tf.step)
	if err != nil {
		return nil, getHistoryOut{}, err
	}
	if rows == nil {
		rows = []store.TimelineRow{}
	}
	return nil, getHistoryOut{From: from, To: to, Step: tf.step.String(), Rows: rows}, nil
}

// --- list_changes ---

type listChangesIn struct {
	From            string `json:"from,omitempty" jsonschema:"optional RFC3339 start of the window"`
	To              string `json:"to,omitempty" jsonschema:"optional RFC3339 end of the window"`
	Cluster         string `json:"cluster,omitempty" jsonschema:"optional cluster name"`
	Limit           int    `json:"limit,omitempty" jsonschema:"most changes to return, default 100, at most 500"`
	IncludeCounters bool   `json:"includeCounters,omitempty" jsonschema:"include updates of counts (pods, nodes, capacity), which change often"`
}

type listChangesOut struct {
	Changes []model.Change `json:"changes"`
}

func (s *Server) toolListChanges(_ context.Context, _ *mcp.CallToolRequest, in listChangesIn) (*mcp.CallToolResult, listChangesOut, error) {
	q := store.ChangeQuery{Cluster: in.Cluster, Limit: 100, ExcludeCounters: !in.IncludeCounters}
	if in.Limit > 0 {
		q.Limit = min(in.Limit, defaultChangeLimit)
	}
	for _, p := range []struct {
		raw string
		dst *time.Time
	}{{in.From, &q.From}, {in.To, &q.To}} {
		if p.raw == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, p.raw)
		if err != nil {
			return nil, listChangesOut{}, errors.New("from and to must be RFC3339 times")
		}
		*p.dst = t
	}
	changes, err := s.Store.Changes(q)
	if err != nil {
		return nil, listChangesOut{}, err
	}
	if changes == nil {
		changes = []model.Change{}
	}
	return nil, listChangesOut{Changes: changes}, nil
}

// --- refresh ---

type refreshOut struct {
	Status string `json:"status"`
}

func (s *Server) toolRefresh(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, refreshOut, error) {
	if s.Scheduler == nil {
		return nil, refreshOut{}, errors.New("this server has no scheduler")
	}
	logging.For("api").Info("refresh requested over MCP, scraping now")
	go s.Scheduler.ScrapeAll(context.Background())
	return nil, refreshOut{Status: "scrape started"}, nil
}
