package routekit

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

type manifestResponse struct {
	status int
	body   string
}

type capturedManifestRequest struct {
	method string
	query  string
	header http.Header
	body   string
}

type manifestServer struct {
	*httptest.Server
	mu        sync.Mutex
	responses []manifestResponse
	requests  []capturedManifestRequest
}

// newManifestServer answers with the given responses in order, repeating the
// last one once they run out.
func newManifestServer(t *testing.T, responses ...manifestResponse) *manifestServer {
	t.Helper()
	server := &manifestServer{responses: responses}
	server.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		server.mu.Lock()
		server.requests = append(server.requests, capturedManifestRequest{
			method: r.Method, query: r.URL.RawQuery, header: r.Header.Clone(), body: string(body),
		})
		index := min(len(server.requests), len(server.responses)) - 1
		response := server.responses[index]
		server.mu.Unlock()
		w.WriteHeader(response.status)
		_, _ = w.Write([]byte(response.body))
	}))
	t.Cleanup(server.Close)
	return server
}

func (s *manifestServer) captured() []capturedManifestRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]capturedManifestRequest(nil), s.requests...)
}

func fastRetry() RetryPolicy {
	return RetryPolicy{Attempts: 3, InitialDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond}
}

func waitManifestResults(t *testing.T, results <-chan ManifestSyncResult, count int) []ManifestSyncResult {
	t.Helper()
	collected := make([]ManifestSyncResult, 0, count)
	for len(collected) < count {
		select {
		case result := <-results:
			collected = append(collected, result)
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for manifest result %d of %d", len(collected)+1, count)
		}
	}
	return collected
}

func assertNoMoreManifestResults(t *testing.T, results <-chan ManifestSyncResult) {
	t.Helper()
	select {
	case result := <-results:
		t.Fatalf("unexpected extra manifest attempt: %+v", result)
	case <-time.After(50 * time.Millisecond):
	}
}

func singleManifestRoute(routeID int32) []Route {
	return []Route{{
		Path:  "/api",
		Group: "api",
		Handlers: []Handler{{
			Method: http.MethodGet, RelativePath: "/resource", Definition: "resource", RouteId: routeID,
		}},
	}}
}

func TestPadraoBodyFormatGolden(t *testing.T) {
	engine := newEngine()
	items := NewRouterGroup(engine, "/api/v1")
	items.GET("/items/", okHandler, "List items", 1002)
	items.POST("/items", okHandler, "Create item", 1001).
		Category(" catalog ").
		AccessTypes("admin").
		Scopes("items:write").
		RequireClientContext().
		IntegrationRoute().
		M2MRoute().
		BasicRoute().
		Restrict()
	auth := NewRouterGroup(engine, "/")
	auth.POST("/auth/login", okHandler, "Login", 5).Public()

	routes := []Route{
		items.Export("items", 1),
		auth.Export("auth", 1),
		{
			// Hand-built metadata: nil bool pointers and nil slices use the
			// addRoute defaults.
			Path:     "/legacy",
			Group:    "legacy",
			Handlers: []Handler{{Method: http.MethodGet, RelativePath: "/ping", Definition: "Ping", RouteId: 7}},
		},
	}

	body, err := PadraoBodyFormat().Build(ManifestInput{Version: "1.4.0", Routes: routes})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	want := `{"version":"1.4.0","routes":[` +
		`{"route_id":5,"method":"POST","path":"/auth/login","definition":"Login","category":"auth","is_authentication":false,"is_authorization":false,"requires_client_context":false,"access_types":[],"scopes":[],"is_integration":false,"m2m":false,"is_basic":false,"restricted":false},` +
		`{"route_id":7,"method":"GET","path":"/legacy/ping","definition":"Ping","category":"legacy","is_authentication":true,"is_authorization":true,"requires_client_context":false,"access_types":[],"scopes":[],"is_integration":false,"m2m":false,"is_basic":false,"restricted":false},` +
		`{"route_id":1001,"method":"POST","path":"/api/v1/items","definition":"Create item","category":"catalog","is_authentication":true,"is_authorization":true,"requires_client_context":true,"access_types":["admin"],"scopes":["items:write"],"is_integration":true,"m2m":true,"is_basic":true,"restricted":true},` +
		`{"route_id":1002,"method":"GET","path":"/api/v1/items/","definition":"List items","category":"items","is_authentication":true,"is_authorization":true,"requires_client_context":false,"access_types":[],"scopes":[],"is_integration":false,"m2m":false,"is_basic":false,"restricted":false}` +
		`]}`
	if string(body) != want {
		t.Errorf("Build() body mismatch\n got: %s\nwant: %s", body, want)
	}
}

func TestPadraoBodyFormatAccumulatesBuildErrors(t *testing.T) {
	routes := []Route{{
		Path:  "/api",
		Group: "api",
		Handlers: []Handler{
			{Method: http.MethodGet, RelativePath: "/zero", RouteId: 0},
			{Method: http.MethodGet, RelativePath: "/a", RouteId: 10},
			{Method: http.MethodGet, RelativePath: "/b", RouteId: 10},
		},
	}}

	_, err := PadraoBodyFormat().Build(ManifestInput{Version: " ", Routes: routes})
	var buildErr *ManifestBuildError
	if !errors.As(err, &buildErr) {
		t.Fatalf("Build() error = %v, want *ManifestBuildError", err)
	}
	if len(buildErr.Errors) != 3 {
		t.Fatalf("got %d build errors, want 3: %v", len(buildErr.Errors), buildErr)
	}
	if buildErr.Errors[0].Field != "version" {
		t.Errorf("first error field = %q, want version", buildErr.Errors[0].Field)
	}
	for i, wantIndex := range []int{0, 2} {
		got := buildErr.Errors[i+1]
		if got.Field != "route_id" || got.RouteIndex == nil || *got.RouteIndex != wantIndex {
			t.Errorf("error %d = %+v, want route_id error at index %d", i+1, got, wantIndex)
		}
	}
}

func TestNewManifestSyncerValidatesConfig(t *testing.T) {
	tests := []struct {
		name   string
		config ManifestSyncConfig
		want   string
	}{
		{"relative URL", ManifestSyncConfig{URL: "/routes", Version: "1.0.0"}, "absolute http or https URL"},
		{"unsupported scheme", ManifestSyncConfig{URL: "ftp://example.com/routes", Version: "1.0.0"}, "absolute http or https URL"},
		{"invalid method", ManifestSyncConfig{URL: "https://example.com/routes", Method: "put", Version: "1.0.0"}, `unsupported manifest sync method "put"`},
		{"empty version", ManifestSyncConfig{URL: "https://example.com/routes", Version: "  "}, "version is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewManifestSyncer(tt.config)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("NewManifestSyncer() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestManifestSyncerSendsConfiguredRequest(t *testing.T) {
	server := newManifestServer(t, manifestResponse{http.StatusOK, `{"success":true,"data":{"status":"applied"}}`})
	headers := http.Header{
		"Authorization": {"Bearer token"},
		"X-Trace":       {"a", "b"},
		"Content-Type":  {"text/plain"},
	}
	syncer, err := NewManifestSyncer(ManifestSyncConfig{
		URL:      server.URL + "/routes?app=billing&dry_run=true",
		Headers:  headers,
		Version:  "1.0.0",
		Blocking: true,
	})
	if err != nil {
		t.Fatalf("NewManifestSyncer() error = %v", err)
	}
	headers.Set("Authorization", "changed after construction")

	if err := syncer.SyncRoutes(nil, singleManifestRoute(1)); err != nil {
		t.Fatalf("SyncRoutes() error = %v", err)
	}

	requests := server.captured()
	if len(requests) != 1 {
		t.Fatalf("got %d requests, want 1", len(requests))
	}
	got := requests[0]
	if got.method != http.MethodPut {
		t.Errorf("method = %s, want PUT", got.method)
	}
	if got.query != "app=billing&dry_run=true" {
		t.Errorf("query = %q, want app=billing&dry_run=true", got.query)
	}
	if got.header.Get("Authorization") != "Bearer token" {
		t.Errorf("Authorization = %q, want Bearer token", got.header.Get("Authorization"))
	}
	if values := got.header.Values("X-Trace"); !slices.Equal(values, []string{"a", "b"}) {
		t.Errorf("X-Trace = %v, want [a b]", values)
	}
	if got.header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got.header.Get("Content-Type"))
	}
	if !strings.HasPrefix(got.body, `{"version":"1.0.0","routes":[{"route_id":1,`) {
		t.Errorf("unexpected body %s", got.body)
	}
}

func TestManifestSyncerBuildErrorsAreReturnedInBothModes(t *testing.T) {
	for _, blocking := range []bool{true, false} {
		server := newManifestServer(t, manifestResponse{http.StatusOK, `{}`})
		syncer, err := NewManifestSyncer(ManifestSyncConfig{URL: server.URL, Version: "1.0.0", Blocking: blocking})
		if err != nil {
			t.Fatalf("NewManifestSyncer() error = %v", err)
		}
		err = syncer.SyncRoutes(nil, singleManifestRoute(0))
		var buildErr *ManifestBuildError
		if !errors.As(err, &buildErr) {
			t.Errorf("blocking=%v: SyncRoutes() error = %v, want *ManifestBuildError", blocking, err)
		}
		if requests := server.captured(); len(requests) != 0 {
			t.Errorf("blocking=%v: got %d requests, want none", blocking, len(requests))
		}
	}
}

func TestManifestSyncerBlockingRejectedThroughAppRouter(t *testing.T) {
	server := newManifestServer(t, manifestResponse{http.StatusUnprocessableEntity, `{
		"success": false,
		"message": "invalid manifest",
		"code": "INVALID_MANIFEST",
		"errors": [{"route_index": 0, "route_id": 1001, "field": "access_types", "message": "unknown access type"}],
		"details": {"invalid": 1}
	}`})
	var results []ManifestSyncResult
	syncer, err := NewManifestSyncer(ManifestSyncConfig{
		URL:      server.URL,
		Version:  "1.0.0",
		Blocking: true,
		OnResult: func(result ManifestSyncResult) { results = append(results, result) },
	})
	if err != nil {
		t.Fatalf("NewManifestSyncer() error = %v", err)
	}

	engine := newEngine()
	group := newGroup(t, engine, "/api/v1", "items", 1)
	group.group.POST("/items", okHandler, "Create item", 1001).AccessTypes("unknown")
	appRouter := NewAppRouterFromRegistrars([]RouteRegistrar{group}, syncer)

	err = appRouter.RegisterRoutes(engine)
	var failure *ManifestSyncFailure
	if !errors.As(err, &failure) {
		t.Fatalf("RegisterRoutes() error = %v, want *ManifestSyncFailure", err)
	}
	result := failure.Result
	if result.Status != ManifestStatusRejected || result.StatusCode != http.StatusUnprocessableEntity || result.Attempt != 1 {
		t.Errorf("result = %+v, want rejected 422 attempt 1", result)
	}
	if result.Message != "invalid manifest" || result.Code != "INVALID_MANIFEST" {
		t.Errorf("message/code = %q/%q", result.Message, result.Code)
	}
	if result.Details["invalid"] != float64(1) {
		t.Errorf("Details = %v", result.Details)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("Errors = %+v, want 1 entry", result.Errors)
	}
	routeError := result.Errors[0]
	if routeError.RouteIndex == nil || *routeError.RouteIndex != 0 || routeError.RouteID == nil || *routeError.RouteID != 1001 ||
		routeError.Field != "access_types" || routeError.Message != "unknown access type" {
		t.Errorf("Errors[0] = %+v", routeError)
	}
	if len(result.Body) == 0 {
		t.Error("expected raw response body")
	}
	if len(results) != 1 {
		t.Errorf("OnResult called %d times, want 1", len(results))
	}
	if requests := server.captured(); len(requests) != 1 {
		t.Errorf("blocking mode sent %d requests, want 1", len(requests))
	}
}

func TestManifestSyncerBlockingUnstructuredErrors(t *testing.T) {
	server := newManifestServer(t, manifestResponse{http.StatusBadRequest, `{"success":false,"message":"bad","errors":["CODE"]}`})
	syncer, err := NewManifestSyncer(ManifestSyncConfig{URL: server.URL, Version: "1.0.0", Blocking: true})
	if err != nil {
		t.Fatalf("NewManifestSyncer() error = %v", err)
	}
	err = syncer.SyncRoutes(nil, singleManifestRoute(1))
	var failure *ManifestSyncFailure
	if !errors.As(err, &failure) {
		t.Fatalf("SyncRoutes() error = %v, want *ManifestSyncFailure", err)
	}
	if failure.Result.Status != ManifestStatusRejected || failure.Result.Message != "bad" || len(failure.Result.Errors) != 0 {
		t.Errorf("result = %+v, want rejected without structured errors", failure.Result)
	}
}

func TestManifestSyncerBlockingSuccessStatuses(t *testing.T) {
	tests := []struct {
		body string
		want string
	}{
		{`{"success":true,"message":"ok","data":{"status":"applied","created":2}}`, ManifestStatusApplied},
		{`{"success":true,"data":{"status":"unchanged"}}`, ManifestStatusUnchanged},
		{`{"success":true,"data":{"status":"valid"}}`, ManifestStatusValid},
		{`{"success":true,"data":{"status":"something-new"}}`, ManifestStatusApplied},
		{`not json`, ManifestStatusApplied},
	}
	for _, tt := range tests {
		server := newManifestServer(t, manifestResponse{http.StatusOK, tt.body})
		var got ManifestSyncResult
		syncer, err := NewManifestSyncer(ManifestSyncConfig{
			URL: server.URL, Version: "1.0.0", Blocking: true,
			OnResult: func(result ManifestSyncResult) { got = result },
		})
		if err != nil {
			t.Fatalf("NewManifestSyncer() error = %v", err)
		}
		if err := syncer.SyncRoutes(nil, singleManifestRoute(1)); err != nil {
			t.Fatalf("body %s: SyncRoutes() error = %v", tt.body, err)
		}
		if got.Status != tt.want || got.StatusCode != http.StatusOK || got.Attempt != 1 {
			t.Errorf("body %s: result = %+v, want status %s", tt.body, got, tt.want)
		}
	}
}

func TestManifestSyncerBlockingNetworkError(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	url := server.URL
	server.Close()

	syncer, err := NewManifestSyncer(ManifestSyncConfig{URL: url, Version: "1.0.0", Blocking: true})
	if err != nil {
		t.Fatalf("NewManifestSyncer() error = %v", err)
	}
	err = syncer.SyncRoutes(nil, singleManifestRoute(1))
	var failure *ManifestSyncFailure
	if !errors.As(err, &failure) {
		t.Fatalf("SyncRoutes() error = %v, want *ManifestSyncFailure", err)
	}
	if failure.Result.Err == nil || failure.Result.Status != ManifestStatusError || errors.Unwrap(failure) == nil {
		t.Errorf("result = %+v, want network error", failure.Result)
	}
}

func TestManifestSyncerBackgroundRetriesTransientFailures(t *testing.T) {
	for _, transient := range []int{http.StatusServiceUnavailable, http.StatusTooManyRequests} {
		server := newManifestServer(t,
			manifestResponse{transient, `{"success":false,"message":"try later"}`},
			manifestResponse{http.StatusOK, `{"success":true,"data":{"status":"applied"}}`},
		)
		results := make(chan ManifestSyncResult, 10)
		syncer, err := NewManifestSyncer(ManifestSyncConfig{
			URL: server.URL, Version: "1.0.0", Retry: fastRetry(),
			OnResult: func(result ManifestSyncResult) { results <- result },
		})
		if err != nil {
			t.Fatalf("NewManifestSyncer() error = %v", err)
		}
		if err := syncer.SyncRoutes(nil, singleManifestRoute(1)); err != nil {
			t.Fatalf("SyncRoutes() error = %v, want nil in background mode", err)
		}

		got := waitManifestResults(t, results, 2)
		assertNoMoreManifestResults(t, results)
		if got[0].Attempt != 1 || got[0].StatusCode != transient {
			t.Errorf("status %d: first result = %+v", transient, got[0])
		}
		if got[1].Attempt != 2 || got[1].StatusCode != http.StatusOK || got[1].Status != ManifestStatusApplied {
			t.Errorf("status %d: second result = %+v", transient, got[1])
		}
	}
}

func TestManifestSyncerBackgroundStopsOnClientError(t *testing.T) {
	server := newManifestServer(t, manifestResponse{http.StatusUnprocessableEntity, `{"success":false,"message":"invalid"}`})
	results := make(chan ManifestSyncResult, 10)
	syncer, err := NewManifestSyncer(ManifestSyncConfig{
		URL: server.URL, Version: "1.0.0", Retry: fastRetry(),
		OnResult: func(result ManifestSyncResult) { results <- result },
	})
	if err != nil {
		t.Fatalf("NewManifestSyncer() error = %v", err)
	}
	if err := syncer.SyncRoutes(nil, singleManifestRoute(1)); err != nil {
		t.Fatalf("SyncRoutes() error = %v", err)
	}

	got := waitManifestResults(t, results, 1)
	assertNoMoreManifestResults(t, results)
	if got[0].Status != ManifestStatusRejected || got[0].Attempt != 1 {
		t.Errorf("result = %+v, want rejected on attempt 1", got[0])
	}
}

func TestManifestSyncerBackgroundGivesUpAfterAttempts(t *testing.T) {
	server := newManifestServer(t, manifestResponse{http.StatusInternalServerError, `{"success":false}`})
	results := make(chan ManifestSyncResult, 10)
	syncer, err := NewManifestSyncer(ManifestSyncConfig{
		URL: server.URL, Version: "1.0.0", Retry: fastRetry(),
		OnResult: func(result ManifestSyncResult) { results <- result },
	})
	if err != nil {
		t.Fatalf("NewManifestSyncer() error = %v", err)
	}
	if err := syncer.SyncRoutes(nil, singleManifestRoute(1)); err != nil {
		t.Fatalf("SyncRoutes() error = %v", err)
	}

	got := waitManifestResults(t, results, 3)
	assertNoMoreManifestResults(t, results)
	for i, result := range got {
		if result.Attempt != i+1 || result.Status != ManifestStatusError {
			t.Errorf("result %d = %+v, want error on attempt %d", i, result, i+1)
		}
	}
}

func TestNormalizeRetryPolicyFillsZeroFields(t *testing.T) {
	got := normalizeRetryPolicy(RetryPolicy{Attempts: 2})
	want := RetryPolicy{Attempts: 2, InitialDelay: 2 * time.Second, MaxDelay: time.Minute}
	if got != want {
		t.Errorf("normalizeRetryPolicy() = %+v, want %+v", got, want)
	}
}

type recordingSyncer struct {
	name  string
	err   error
	calls *[]string
}

func (s recordingSyncer) SyncRoutes([]int32, []Route) error {
	*s.calls = append(*s.calls, s.name)
	return s.err
}

func TestChainRouteSyncers(t *testing.T) {
	var calls []string
	failure := errors.New("second failed")
	chain := ChainRouteSyncers(
		recordingSyncer{name: "first", calls: &calls},
		nil,
		recordingSyncer{name: "second", err: failure, calls: &calls},
		recordingSyncer{name: "third", calls: &calls},
	)

	if err := chain.SyncRoutes(nil, nil); !errors.Is(err, failure) {
		t.Fatalf("SyncRoutes() error = %v, want %v", err, failure)
	}
	if want := []string{"first", "second"}; !slices.Equal(calls, want) {
		t.Errorf("calls = %v, want %v", calls, want)
	}

	calls = nil
	if err := ChainRouteSyncers(nil, recordingSyncer{name: "only", calls: &calls}).SyncRoutes(nil, nil); err != nil {
		t.Fatalf("SyncRoutes() error = %v", err)
	}
	if want := []string{"only"}; !slices.Equal(calls, want) {
		t.Errorf("calls = %v, want %v", calls, want)
	}
}
