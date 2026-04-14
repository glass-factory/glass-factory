// Package sentinel provides config-driven health monitoring for Glass Factory services.
// sentinel 包提供 Glass Factory 服务的配置驱动健康监控。
//
// Each Check defines a URL endpoint to probe on a recurring interval.
// Results are collected and the most recent result per check is always available
// via LastResults. Failures trigger the OnFailure callback for alerting or logging.
//
// 每个 Check 定义一个在循环间隔内探测的 URL 端点。
// 结果被收集，每个检查的最新结果始终可通过 LastResults 获取。
// 失败会触发 OnFailure 回调，用于告警或日志记录。
package sentinel

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Check describes a single health check endpoint and its polling parameters.
type Check struct {
	// Name is a human-readable identifier for this check.
	Name string
	// URL is the endpoint to probe.
	URL string
	// Method is the HTTP method to use. Defaults to GET if empty.
	Method string
	// ExpectedStatus is the HTTP status code that indicates success. Defaults to 200 if zero.
	ExpectedStatus int
	// IntervalSecs is how often to run this check, in seconds. Defaults to 60 if zero.
	IntervalSecs int
	// TimeoutSecs is the HTTP request timeout, in seconds. Defaults to 10 if zero.
	TimeoutSecs int
}

// CheckResult holds the outcome of a single health check execution.
type CheckResult struct {
	// CheckName identifies which check produced this result.
	CheckName string
	// URL is the endpoint that was probed.
	URL string
	// Status is the HTTP status code received, or 0 if the request failed.
	Status int
	// Latency is how long the check took to complete.
	Latency time.Duration
	// OK is true when the check passed (status matched expected).
	OK bool
	// Error contains an error message if the check failed, or empty string on success.
	Error string
	// Timestamp is when this check was executed.
	Timestamp time.Time
}

// Sentinel runs periodic health checks against configured endpoints.
type Sentinel struct {
	checks    []Check
	onFailure func(CheckResult)
	results   chan CheckResult

	mu          sync.RWMutex
	lastResults map[string]CheckResult
}

// New creates a Sentinel with the given checks and failure callback.
// Zero-value fields in each Check are replaced with defaults:
// Method defaults to "GET", ExpectedStatus to 200, IntervalSecs to 60, TimeoutSecs to 10.
func New(checks []Check, onFailure func(CheckResult)) *Sentinel {
	normalised := make([]Check, len(checks))
	for i, c := range checks {
		if c.Method == "" {
			c.Method = "GET"
		}
		if c.ExpectedStatus == 0 {
			c.ExpectedStatus = 200
		}
		if c.IntervalSecs == 0 {
			c.IntervalSecs = 60
		}
		if c.TimeoutSecs == 0 {
			c.TimeoutSecs = 10
		}
		normalised[i] = c
	}

	return &Sentinel{
		checks:      normalised,
		onFailure:   onFailure,
		results:     make(chan CheckResult, len(normalised)*2),
		lastResults: make(map[string]CheckResult),
	}
}

// Run starts a goroutine per check, each on its own ticker. It also starts a
// collector goroutine that reads results from the internal channel and stores
// them. Run blocks until ctx is cancelled.
func (s *Sentinel) Run(ctx context.Context) {
	var wg sync.WaitGroup

	// Collector goroutine: reads from the results channel and stores last results.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case r := <-s.results:
				s.mu.Lock()
				s.lastResults[r.CheckName] = r
				s.mu.Unlock()
				if !r.OK && s.onFailure != nil {
					s.onFailure(r)
				}
			}
		}
	}()

	// One goroutine per check on its own ticker.
	for _, c := range s.checks {
		wg.Add(1)
		go func(check Check) {
			defer wg.Done()

			// Run immediately on start, then on the ticker.
			result := RunCheck(ctx, check)
			select {
			case s.results <- result:
			case <-ctx.Done():
				return
			}

			ticker := time.NewTicker(time.Duration(check.IntervalSecs) * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					r := RunCheck(ctx, check)
					select {
					case s.results <- r:
					case <-ctx.Done():
						return
					}
				}
			}
		}(c)
	}

	wg.Wait()
}

// RunCheck executes a single health check and returns the result.
// This is exported so callers can run on-demand checks outside the ticker loop.
func RunCheck(ctx context.Context, check Check) CheckResult {
	// Apply defaults for ad-hoc usage where caller may not have called New.
	if check.Method == "" {
		check.Method = "GET"
	}
	if check.ExpectedStatus == 0 {
		check.ExpectedStatus = 200
	}
	if check.TimeoutSecs == 0 {
		check.TimeoutSecs = 10
	}

	result := CheckResult{
		CheckName: check.Name,
		URL:       check.URL,
		Timestamp: time.Now(),
	}

	timeout := time.Duration(check.TimeoutSecs) * time.Second
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, check.Method, check.URL, nil)
	if err != nil {
		result.Error = fmt.Errorf("creating request: %w", err).Error()
		return result
	}

	client := &http.Client{}
	start := time.Now()
	resp, err := client.Do(req)
	result.Latency = time.Since(start)

	if err != nil {
		result.Error = fmt.Errorf("executing request: %w", err).Error()
		return result
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	result.Status = resp.StatusCode
	if resp.StatusCode == check.ExpectedStatus {
		result.OK = true
	} else {
		result.Error = fmt.Sprintf("expected status %d, got %d", check.ExpectedStatus, resp.StatusCode)
	}

	return result
}

// LastResults returns the most recent CheckResult for every check, keyed by name.
// It is safe to call from multiple goroutines.
func (s *Sentinel) LastResults() []CheckResult {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]CheckResult, 0, len(s.lastResults))
	for _, r := range s.lastResults {
		out = append(out, r)
	}
	return out
}
