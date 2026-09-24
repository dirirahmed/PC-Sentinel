package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dirirahmed/pc-sentinel/internal/analyzer"
	"github.com/dirirahmed/pc-sentinel/internal/config"
	"github.com/dirirahmed/pc-sentinel/internal/models"
	"github.com/dirirahmed/pc-sentinel/internal/storage"
)

type fakeSource struct {
	snap        *models.Snapshot
	points      []models.HistoryPoint
	seriesErr   error
	procErr     error
	reconfigged bool
}

func (f *fakeSource) Snapshot() (models.Snapshot, bool) {
	if f.snap == nil {
		return models.Snapshot{}, false
	}
	return *f.snap, true
}
func (f *fakeSource) Health() analyzer.HealthReport {
	return analyzer.HealthReport{Overall: 88, Grade: "good"}
}
func (f *fakeSource) ActiveAlerts() []models.Alert {
	return []models.Alert{{ID: "a", Rule: "cpu_usage", Severity: models.SeverityWarning}}
}
func (f *fakeSource) AlertHistory(context.Context, time.Time, int) ([]models.Alert, error) {
	return nil, storage.ErrUnavailable
}
func (f *fakeSource) Series(context.Context, time.Duration) ([]models.HistoryPoint, time.Duration, error) {
	return f.points, 2 * time.Second, f.seriesErr
}
func (f *fakeSource) DiskHistory(context.Context, time.Duration) ([]models.DiskUsagePoint, error) {
	return []models.DiskUsagePoint{{Mountpoint: "C:"}}, nil
}
func (f *fakeSource) Processes(context.Context) ([]models.Process, error) {
	if f.procErr != nil {
		return nil, f.procErr
	}
	return []models.Process{{PID: 42, Name: "code.exe"}}, nil
}
func (f *fakeSource) Host() models.HostInfo    { return models.HostInfo{Hostname: "test"} }
func (f *fakeSource) Agent() models.AgentStats { return models.AgentStats{PID: 1} }
func (f *fakeSource) Reconfigure()             { f.reconfigged = true }

func newTestServer(t *testing.T, src *fakeSource, static fs.FS) (http.Handler, *config.Store) {
	t.Helper()
	cfg := config.NewStore(filepath.Join(t.TempDir(), "config.json"), config.Default())
	return New(src, cfg, static, nil).Handler(), cfg
}

func sampleSnapshot() *models.Snapshot {
	return &models.Snapshot{
		Timestamp: time.Now(),
		CPU:       models.CPUStats{Model: "Test CPU", UsagePercent: 42},
		Memory:    models.MemoryStats{UsedPercent: 55},
		GPU:       models.GPUReport{Available: false, Reason: "none", Adapters: []models.GPUStats{}},
		Disks:     []models.DiskStats{{Mountpoint: "C:"}},
	}
}

func do(t *testing.T, h http.Handler, method, target, body string, header map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, r)
	req.Host = "127.0.0.1:8787"
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("invalid JSON %q: %v", rec.Body.String(), err)
	}
	return v
}

func TestServiceUnavailableBeforeFirstSample(t *testing.T) {
	h, _ := newTestServer(t, &fakeSource{}, nil)
	for _, path := range []string{"/api/system", "/api/cpu", "/api/memory", "/api/gpu", "/api/disks", "/api/network", "/api/health"} {
		rec := do(t, h, "GET", path, "", nil)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: status %d, want 503", path, rec.Code)
		}
		if decode[errorResponse](t, rec).Error == "" {
			t.Errorf("%s: error message missing", path)
		}
	}
}

func TestSystemEndpoint(t *testing.T) {
	h, _ := newTestServer(t, &fakeSource{snap: sampleSnapshot()}, nil)
	rec := do(t, h, "GET", "/api/system", "", nil)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type %q", ct)
	}
	got := decode[SystemResponse](t, rec)
	if got.CPU.UsagePercent != 42 || got.Health.Overall != 88 || got.Host.Hostname != "test" || len(got.ActiveAlerts) != 1 {
		t.Errorf("unexpected body: %+v", got)
	}
	// Unavailable metrics must serialize as null, not 0.
	if !strings.Contains(rec.Body.String(), `"temperatureC":null`) {
		t.Error("missing temperature should be null")
	}
}

func TestCPUEndpointIncludesSummary(t *testing.T) {
	src := &fakeSource{snap: sampleSnapshot(), points: []models.HistoryPoint{{CPU: 10, CPUMax: 30}, {CPU: 30, CPUMax: 40}}}
	h, _ := newTestServer(t, src, nil)
	got := decode[ComponentResponse[models.CPUStats]](t, do(t, h, "GET", "/api/cpu?range=5m", "", nil))
	if got.Summary == nil || got.Summary.Avg != 20 || got.Summary.Peak != 40 || got.Range != "5m" {
		t.Errorf("summary = %+v range=%s", got.Summary, got.Range)
	}
}

func TestComponentStillWorksWhenHistoryFails(t *testing.T) {
	src := &fakeSource{snap: sampleSnapshot(), seriesErr: errors.New("db gone")}
	h, _ := newTestServer(t, src, nil)
	rec := do(t, h, "GET", "/api/memory", "", nil)
	got := decode[ComponentResponse[models.MemoryStats]](t, rec)
	if rec.Code != 200 || got.Current.UsedPercent != 55 || got.Summary != nil {
		t.Errorf("status %d body %+v", rec.Code, got)
	}
}

func TestMetricsEndpoint(t *testing.T) {
	src := &fakeSource{snap: sampleSnapshot(), points: []models.HistoryPoint{{CPU: 5}}}
	h, _ := newTestServer(t, src, nil)

	got := decode[MetricsResponse](t, do(t, h, "GET", "/api/metrics?range=1h&disks=1", "", nil))
	if got.Range != "1h" || len(got.Points) != 1 || len(got.Disks) != 1 || got.BucketSeconds != 2 {
		t.Errorf("unexpected: %+v", got)
	}
	if rec := do(t, h, "GET", "/api/metrics?range=1y", "", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("bad range: status %d", rec.Code)
	}
	src.seriesErr = storage.ErrUnavailable
	if rec := do(t, h, "GET", "/api/metrics?range=7d", "", nil); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("storage down: status %d", rec.Code)
	}
}

func TestProcessesEndpoint(t *testing.T) {
	src := &fakeSource{}
	h, _ := newTestServer(t, src, nil)
	got := decode[ProcessesResponse](t, do(t, h, "GET", "/api/processes", "", nil))
	if got.Count != 1 || got.Processes[0].Name != "code.exe" || !strings.Contains(got.Note, "not supported") {
		t.Errorf("unexpected: %+v", got)
	}
	src.procErr = errors.New("access denied")
	if rec := do(t, h, "GET", "/api/processes", "", nil); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status %d", rec.Code)
	}
}

func TestAlertsEndpointDegradesWithoutHistory(t *testing.T) {
	h, _ := newTestServer(t, &fakeSource{}, nil)
	rec := do(t, h, "GET", "/api/alerts", "", nil)
	got := decode[AlertsResponse](t, rec)
	if rec.Code != 200 || len(got.Active) != 1 || got.HistoryError == "" || got.History == nil {
		t.Errorf("status %d body %+v", rec.Code, got)
	}
	if rec := do(t, h, "GET", "/api/alerts?limit=0", "", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("invalid limit accepted: %d", rec.Code)
	}
}

func TestSettingsUpdate(t *testing.T) {
	src := &fakeSource{}
	h, cfg := newTestServer(t, src, nil)
	jsonHdr := map[string]string{"Content-Type": "application/json"}

	rec := do(t, h, "PUT", "/api/settings", `{"sampleIntervalSeconds": 5, "port": 9999}`, jsonHdr)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	got := decode[SettingsResponse](t, rec)
	if cfg.Get().SampleIntervalSeconds != 5 || !src.reconfigged {
		t.Error("settings not applied or monitor not notified")
	}
	if len(got.RestartRequired) != 1 || got.RestartRequired[0] != "port" {
		t.Errorf("restartRequired = %v", got.RestartRequired)
	}
	if cfg.Get().RetentionDays != config.Default().RetentionDays {
		t.Error("partial update must keep other fields")
	}

	cases := []struct {
		body, ctype string
		want        int
	}{
		{`{"retentionDays": 0}`, "application/json", http.StatusUnprocessableEntity},
		{`{"nonsense": 1}`, "application/json", http.StatusBadRequest},
		{`not json`, "application/json", http.StatusBadRequest},
		{`{"retentionDays": 3}`, "text/plain", http.StatusUnsupportedMediaType},
	}
	for _, c := range cases {
		if rec := do(t, h, "PUT", "/api/settings", c.body, map[string]string{"Content-Type": c.ctype}); rec.Code != c.want {
			t.Errorf("body %q: status %d, want %d", c.body, rec.Code, c.want)
		}
	}
	if cfg.Get().RetentionDays != config.Default().RetentionDays {
		t.Error("rejected updates must not change config")
	}
}

func TestForeignHostRejected(t *testing.T) {
	h, _ := newTestServer(t, &fakeSource{snap: sampleSnapshot()}, nil)
	req := httptest.NewRequest("GET", "/api/system", nil)
	req.Host = "attacker.example:8787"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status %d, want 403", rec.Code)
	}
	req.Host = "localhost:8787"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("localhost rejected: %d", rec.Code)
	}
}

func TestUnknownAPIRouteAndMethod(t *testing.T) {
	h, _ := newTestServer(t, &fakeSource{}, nil)
	if rec := do(t, h, "GET", "/api/nope", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown route: %d", rec.Code)
	}
	if rec := do(t, h, "DELETE", "/api/processes", "", nil); rec.Code == http.StatusOK {
		t.Error("DELETE on processes must not succeed")
	}
}

func TestStaticSPAFallback(t *testing.T) {
	h, _ := newTestServer(t, &fakeSource{}, fstest.MapFS{
		"index.html":    {Data: []byte("<html>app</html>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	})
	if rec := do(t, h, "GET", "/processes", "", nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), "app") {
		t.Errorf("client route should serve index.html: %d %s", rec.Code, rec.Body)
	}
	rec := do(t, h, "GET", "/assets/app.js", "", nil)
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("asset: %d %v", rec.Code, rec.Header())
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Error("security headers missing")
	}
}
