package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/croz-ltd/periscope/internal/model"
	"github.com/croz-ltd/periscope/internal/store"
)

// mcpFleet is two clusters: prod is behind on OpenShift and both report VM
// capacity.
func mcpFleet(t *testing.T) *Server {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "mcp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	now := time.Now()
	for _, c := range []struct{ name, ocp, vcpu string }{{"prod", "4.16.9", "17864"}, {"dev", "4.17.2", "360"}} {
		snap := model.Snapshot{Cluster: c.name, Time: now, OK: true, Components: []model.Component{
			{Key: "openshift", Name: "OpenShift", Group: model.GroupOpenShift, Compare: model.CompareVersion, Version: c.ocp},
			{Key: "vm-vcpu-total", Name: "VM vCPUs: total", Group: model.GroupCapacity, Compare: model.CompareInfo,
				Version: c.vcpu, Extra: map[string]string{"allocation ratio": "10:1"}},
			{Key: "storage-total", Name: "Storage (all classes)", Group: model.GroupStorage, Compare: model.CompareInfo,
				Version: "3 PVC / 4 PV", Extra: map[string]string{"pvc": "3", "pv": "4"}},
		}}
		if err := st.SaveSnapshot(snap); err != nil {
			t.Fatal(err)
		}
	}
	return &Server{Store: st, StaleAfter: time.Hour}
}

func mcpSession(t *testing.T, h http.Handler) *mcp.ClientSession {
	t.Helper()
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: ts.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

// call runs a tool and decodes its structured result into out.
func call(t *testing.T, s *mcp.ClientSession, tool string, args map[string]any, out any) *mcp.CallToolResult {
	t.Helper()
	res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	if out != nil && !res.IsError {
		raw, _ := json.Marshal(res.StructuredContent)
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("%s result: %v", tool, err)
		}
	}
	return res
}

func TestMCPToolsAreListed(t *testing.T) {
	s := mcpSession(t, mcpFleet(t).Handler())
	res, err := s.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range res.Tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"list_clusters", "list_components", "get_component", "get_cluster",
		"fleet_totals", "find_drift", "get_history", "list_changes", "refresh"} {
		if !names[want] {
			t.Errorf("tool %s is missing", want)
		}
	}
}

func TestMCPFleetTotalsAddsUpTheFleet(t *testing.T) {
	s := mcpSession(t, mcpFleet(t).Handler())
	var out fleetTotalsOut
	call(t, s, "fleet_totals", map[string]any{"keys": []string{"vm-vcpu-total", "storage-total", "openshift"}}, &out)
	if len(out.Totals) != 3 {
		t.Fatalf("totals = %+v", out.Totals)
	}
	vcpu := out.Totals[0]
	if vcpu.Total != 18224 || vcpu.Count != 2 || vcpu.Max.Cluster != "prod" || vcpu.Min.Value != 360 {
		t.Errorf("vCPU total = %+v", vcpu)
	}
	if st := out.Totals[1]; st.PVC == nil || *st.PVC != 6 || *st.PV != 8 {
		t.Errorf("storage total = %+v, want 6 PVC and 8 PV", st)
	}
	if ocp := out.Totals[2]; ocp.Count != 0 || len(ocp.NotCounted) != 2 {
		t.Errorf("a version row was summed: %+v", ocp)
	}

	// Only the named cluster.
	call(t, s, "fleet_totals", map[string]any{"keys": []string{"vm-vcpu-total"}, "clusters": []string{"dev"}}, &out)
	if out.Totals[0].Total != 360 {
		t.Errorf("dev only = %+v", out.Totals[0])
	}
}

func TestMCPFindDriftAndReads(t *testing.T) {
	s := mcpSession(t, mcpFleet(t).Handler())

	var drift findDriftOut
	call(t, s, "find_drift", nil, &drift)
	if len(drift.Items) != 1 || drift.Items[0].Cluster != "prod" || drift.Items[0].Expected != "4.17.2" ||
		drift.Items[0].Issue != "behind" || drift.Summary["behind"] != 1 {
		t.Errorf("drift = %+v", drift)
	}

	var comp getComponentOut
	call(t, s, "get_component", map[string]any{"key": "vm-vcpu-total"}, &comp)
	if len(comp.Cells) != 2 || comp.Cells[0].Details["allocation ratio"] != "10:1" {
		t.Errorf("component = %+v", comp)
	}

	var cl getClusterOut
	call(t, s, "get_cluster", map[string]any{"cluster": "dev", "group": "Capacity"}, &cl)
	if cl.Cluster.Name != "dev" || len(cl.Components) != 1 || cl.Components[0].Value != "360" {
		t.Errorf("cluster = %+v", cl)
	}

	var list listComponentsOut
	call(t, s, "list_components", map[string]any{"query": "vcpu"}, &list)
	if len(list.Components) != 1 || list.Components[0].Reporting != 2 {
		t.Errorf("components = %+v", list.Components)
	}

	// An unknown key fails with a hint, as a tool error the model can read.
	res := call(t, s, "get_component", map[string]any{"key": "vcpu"}, nil)
	text := res.Content[0].(*mcp.TextContent).Text
	if !res.IsError || !strings.Contains(text, "vm-vcpu-total") {
		t.Errorf("unknown key = %v %q, want an error that suggests vm-vcpu-total", res.IsError, text)
	}

	var changes listChangesOut
	call(t, s, "list_changes", map[string]any{"limit": 5}, &changes)
	if changes.Changes == nil {
		t.Error("changes = nil, want a list")
	}
}

// /mcp sits behind the same guard as the REST API.
func TestMCPNeedsAToken(t *testing.T) {
	srv := mcpFleet(t)
	h := guarded(t, srv)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("MCP without a token = %d, want 401", rec.Code)
	}

	ts := httptest.NewServer(h)
	defer ts.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	transport := &mcp.StreamableClientTransport{Endpoint: ts.URL + "/mcp",
		HTTPClient: &http.Client{Transport: bearerTransport{"a-readers-token"}}}
	session, err := client.Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatalf("connect with a reader token: %v", err)
	}
	defer session.Close()
	var out listClustersOut
	call(t, session, "list_clusters", nil, &out)
	if len(out.Clusters) != 2 {
		t.Errorf("clusters = %+v", out.Clusters)
	}
}

type bearerTransport struct{ token string }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}
