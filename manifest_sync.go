package routekit

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// BodyFormat builds the manifest request body and interprets the response.
// New wire formats are added as new implementations.
type BodyFormat interface {
	Name() string
	ContentType() string
	Build(input ManifestInput) ([]byte, error)
	ParseResponse(statusCode int, body []byte) ManifestSyncResult
}

type ManifestInput struct {
	Version string
	Routes  []Route
}

const (
	ManifestStatusApplied   = "applied"
	ManifestStatusUnchanged = "unchanged"
	ManifestStatusValid     = "valid"
	ManifestStatusRejected  = "rejected"
	ManifestStatusError     = "error"
)

const (
	defaultManifestMethod       = http.MethodPut
	defaultManifestTimeout      = 15 * time.Second
	defaultManifestAttempts     = 5
	defaultManifestInitialDelay = 2 * time.Second
	defaultManifestMaxDelay     = time.Minute
	maxManifestResponseBytes    = 1 << 20
)

type ManifestSyncConfig struct {
	// URL is used as given, including its query string.
	URL string
	// Method defaults to PUT.
	Method  string
	Headers http.Header
	// Version is the application version forwarded to the format.
	Version string
	// Format defaults to PadraoBodyFormat().
	Format BodyFormat
	// Blocking makes RegisterRoutes return the sync error after a single
	// attempt. When false, the request runs in a goroutine with retry.
	Blocking bool
	// Client defaults to an http.Client with a 15s timeout.
	Client *http.Client
	// Retry applies only to background mode; zero fields use the defaults.
	Retry RetryPolicy
	// OnResult is called after every finished attempt, in both modes.
	OnResult func(ManifestSyncResult)
}

// RetryPolicy controls background retries. Attempts counts every request,
// including the first one. Zero fields fall back to 5 attempts, 2s initial
// delay and 1min max delay.
type RetryPolicy struct {
	Attempts     int
	InitialDelay time.Duration
	MaxDelay     time.Duration
}

type ManifestSyncResult struct {
	Attempt    int
	StatusCode int
	// Status is one of applied, unchanged, valid, rejected or error.
	Status  string
	Message string
	Code    string
	// Errors holds per-route errors returned by the destination.
	Errors  []ManifestSyncError
	Details map[string]any
	// Body is the raw response body.
	Body []byte
	// Err is the network error; nil when a response was received.
	Err error
}

type ManifestSyncError struct {
	RouteIndex *int
	RouteID    *int32
	Field      string
	Message    string
}

// ManifestSyncFailure is returned by a blocking syncer when the destination
// answers with a non-2xx status or cannot be reached.
type ManifestSyncFailure struct {
	Result ManifestSyncResult
}

func (e *ManifestSyncFailure) Error() string {
	if e.Result.Err != nil {
		return fmt.Sprintf("manifest sync failed: %v", e.Result.Err)
	}
	message := fmt.Sprintf("manifest sync failed: HTTP %d (%s)", e.Result.StatusCode, e.Result.Status)
	if e.Result.Message != "" {
		message += ": " + e.Result.Message
	}
	return message
}

func (e *ManifestSyncFailure) Unwrap() error {
	return e.Result.Err
}

// ManifestBuildError reports problems found while building the manifest body.
type ManifestBuildError struct {
	Errors []ManifestSyncError
}

func (e *ManifestBuildError) Error() string {
	messages := make([]string, 0, len(e.Errors))
	for _, item := range e.Errors {
		messages = append(messages, item.Message)
	}
	return "manifest build failed: " + strings.Join(messages, "; ")
}

type manifestSyncer struct {
	url      string
	method   string
	headers  http.Header
	version  string
	format   BodyFormat
	blocking bool
	client   *http.Client
	retry    RetryPolicy
	onResult func(ManifestSyncResult)
}

// NewManifestSyncer returns a RouteSyncer that sends the route manifest to an
// HTTP endpoint.
func NewManifestSyncer(config ManifestSyncConfig) (RouteSyncer, error) {
	var errs []error

	parsed, err := url.Parse(config.URL)
	if err != nil {
		errs = append(errs, fmt.Errorf("invalid manifest sync URL: %w", err))
	} else if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		errs = append(errs, fmt.Errorf("manifest sync URL must be an absolute http or https URL, got %q", config.URL))
	}

	method := config.Method
	if method == "" {
		method = defaultManifestMethod
	}
	if _, ok := ValidMethods[method]; !ok {
		errs = append(errs, fmt.Errorf("unsupported manifest sync method %q", method))
	}

	if strings.TrimSpace(config.Version) == "" {
		errs = append(errs, errors.New("manifest sync version is required"))
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	format := config.Format
	if format == nil {
		format = PadraoBodyFormat()
	}
	client := config.Client
	if client == nil {
		client = &http.Client{Timeout: defaultManifestTimeout}
	}

	return &manifestSyncer{
		url:      config.URL,
		method:   method,
		headers:  config.Headers.Clone(),
		version:  config.Version,
		format:   format,
		blocking: config.Blocking,
		client:   client,
		retry:    normalizeRetryPolicy(config.Retry),
		onResult: config.OnResult,
	}, nil
}

func normalizeRetryPolicy(policy RetryPolicy) RetryPolicy {
	if policy.Attempts <= 0 {
		policy.Attempts = defaultManifestAttempts
	}
	if policy.InitialDelay <= 0 {
		policy.InitialDelay = defaultManifestInitialDelay
	}
	if policy.MaxDelay <= 0 {
		policy.MaxDelay = defaultManifestMaxDelay
	}
	return policy
}

func (s *manifestSyncer) SyncRoutes(_ []int32, routes []Route) error {
	// Build errors are code bugs, not delivery failures, so they are always
	// returned, even in background mode.
	body, err := s.format.Build(ManifestInput{Version: s.version, Routes: routes})
	if err != nil {
		return err
	}

	if s.blocking {
		result := s.attempt(body, 1)
		s.report(result)
		if result.Err != nil || !isSuccessStatus(result.StatusCode) {
			return &ManifestSyncFailure{Result: result}
		}
		return nil
	}

	go s.runWithRetry(body)
	return nil
}

func (s *manifestSyncer) runWithRetry(body []byte) {
	delay := s.retry.InitialDelay
	for attempt := 1; attempt <= s.retry.Attempts; attempt++ {
		result := s.attempt(body, attempt)
		s.report(result)
		if !shouldRetryManifest(result) || attempt == s.retry.Attempts {
			return
		}
		time.Sleep(delay)
		delay = min(delay*2, s.retry.MaxDelay)
	}
}

func (s *manifestSyncer) attempt(body []byte, attempt int) ManifestSyncResult {
	request, err := http.NewRequest(s.method, s.url, bytes.NewReader(body))
	if err != nil {
		return networkResult(attempt, err)
	}
	request.Header = s.headers.Clone()
	if request.Header == nil {
		request.Header = http.Header{}
	}
	request.Header.Set("Content-Type", s.format.ContentType())

	response, err := s.client.Do(request)
	if err != nil {
		return networkResult(attempt, err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxManifestResponseBytes))
	if err != nil {
		return networkResult(attempt, err)
	}

	result := s.format.ParseResponse(response.StatusCode, responseBody)
	result.Attempt = attempt
	result.StatusCode = response.StatusCode
	result.Body = responseBody
	return result
}

func (s *manifestSyncer) report(result ManifestSyncResult) {
	if s.onResult != nil {
		s.onResult(result)
	}
}

func networkResult(attempt int, err error) ManifestSyncResult {
	return ManifestSyncResult{
		Attempt: attempt,
		Status:  ManifestStatusError,
		Message: err.Error(),
		Err:     err,
	}
}

func isSuccessStatus(statusCode int) bool {
	return statusCode >= 200 && statusCode < 300
}

// shouldRetryManifest retries only network errors, 5xx and 429; other 4xx
// responses are final.
func shouldRetryManifest(result ManifestSyncResult) bool {
	return result.Err != nil || result.StatusCode >= 500 || result.StatusCode == http.StatusTooManyRequests
}

type chainedRouteSyncer []RouteSyncer

// ChainRouteSyncers runs the syncers in order, skipping nil entries, and stops
// at the first error.
func ChainRouteSyncers(syncers ...RouteSyncer) RouteSyncer {
	chain := make(chainedRouteSyncer, 0, len(syncers))
	for _, syncer := range syncers {
		if syncer != nil {
			chain = append(chain, syncer)
		}
	}
	return chain
}

func (c chainedRouteSyncer) SyncRoutes(routeIDs []int32, routes []Route) error {
	for _, syncer := range c {
		if err := syncer.SyncRoutes(routeIDs, routes); err != nil {
			return err
		}
	}
	return nil
}
