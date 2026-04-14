package sentinel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRunCheck_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	check := Check{
		Name:           "healthy",
		URL:            srv.URL,
		ExpectedStatus: 200,
		TimeoutSecs:    5,
	}

	result := RunCheck(context.Background(), check)

	if !result.OK {
		t.Fatalf("expected OK=true, got false; error: %s", result.Error)
	}
	if result.Status != 200 {
		t.Fatalf("expected status 200, got %d", result.Status)
	}
	if result.CheckName != "healthy" {
		t.Fatalf("expected CheckName 'healthy', got %q", result.CheckName)
	}
	if result.Latency <= 0 {
		t.Fatal("expected positive latency")
	}
	if result.Error != "" {
		t.Fatalf("expected no error, got %q", result.Error)
	}
}

func TestRunCheck_Failure(t *testing.T) {
	check := Check{
		Name:        "bad-endpoint",
		URL:         "http://127.0.0.1:1", // nothing listening
		TimeoutSecs: 2,
	}

	result := RunCheck(context.Background(), check)

	if result.OK {
		t.Fatal("expected OK=false for unreachable endpoint")
	}
	if result.Error == "" {
		t.Fatal("expected non-empty error")
	}
	if result.Status != 0 {
		t.Fatalf("expected status 0 for failed request, got %d", result.Status)
	}
}

func TestRunCheck_WrongStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	check := Check{
		Name:           "wrong-status",
		URL:            srv.URL,
		ExpectedStatus: 200,
		TimeoutSecs:    5,
	}

	result := RunCheck(context.Background(), check)

	if result.OK {
		t.Fatal("expected OK=false when status does not match")
	}
	if result.Status != 500 {
		t.Fatalf("expected status 500, got %d", result.Status)
	}
	if result.Error == "" {
		t.Fatal("expected non-empty error describing status mismatch")
	}
}

func TestRunCheck_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	check := Check{
		Name:        "slow-endpoint",
		URL:         srv.URL,
		TimeoutSecs: 1,
	}

	result := RunCheck(context.Background(), check)

	if result.OK {
		t.Fatal("expected OK=false for timed-out request")
	}
	if result.Error == "" {
		t.Fatal("expected non-empty error for timeout")
	}
}

func TestRunCheck_Methods(t *testing.T) {
	tests := []struct {
		name   string
		method string
	}{
		{"GET request", "GET"},
		{"POST request", "POST"},
		{"HEAD request", "HEAD"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotMethod string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			check := Check{
				Name:        tt.name,
				URL:         srv.URL,
				Method:      tt.method,
				TimeoutSecs: 5,
			}

			result := RunCheck(context.Background(), check)

			if !result.OK {
				t.Fatalf("expected OK=true, got false; error: %s", result.Error)
			}
			if gotMethod != tt.method {
				t.Fatalf("expected method %s, got %s", tt.method, gotMethod)
			}
		})
	}
}

func TestLastResults(t *testing.T) {
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv1.Close()

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv2.Close()

	checks := []Check{
		{Name: "svc-a", URL: srv1.URL},
		{Name: "svc-b", URL: srv2.URL},
	}

	s := New(checks, nil)

	// Run checks manually and feed results through the sentinel's internals.
	for _, c := range s.checks {
		r := RunCheck(context.Background(), c)
		s.mu.Lock()
		s.lastResults[r.CheckName] = r
		s.mu.Unlock()
	}

	results := s.LastResults()
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	found := make(map[string]bool)
	for _, r := range results {
		found[r.CheckName] = true
		if !r.OK {
			t.Fatalf("expected OK=true for %s, got false; error: %s", r.CheckName, r.Error)
		}
	}
	if !found["svc-a"] || !found["svc-b"] {
		t.Fatalf("missing check results; found: %v", found)
	}
}

func TestNew_Defaults(t *testing.T) {
	checks := []Check{
		{Name: "bare-minimum", URL: "http://example.com"},
	}

	s := New(checks, nil)

	if len(s.checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(s.checks))
	}

	c := s.checks[0]

	tests := []struct {
		field    string
		got      interface{}
		expected interface{}
	}{
		{"Method", c.Method, "GET"},
		{"ExpectedStatus", c.ExpectedStatus, 200},
		{"IntervalSecs", c.IntervalSecs, 60},
		{"TimeoutSecs", c.TimeoutSecs, 10},
	}

	for _, tt := range tests {
		if tt.got != tt.expected {
			t.Errorf("default %s: expected %v, got %v", tt.field, tt.expected, tt.got)
		}
	}
}

func TestRun_CallsOnFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	failureCh := make(chan CheckResult, 1)

	checks := []Check{
		{
			Name:           "failing-svc",
			URL:            srv.URL,
			ExpectedStatus: 200,
			IntervalSecs:   1,
			TimeoutSecs:    2,
		},
	}

	s := New(checks, func(r CheckResult) {
		select {
		case failureCh <- r:
		default:
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go s.Run(ctx)

	select {
	case r := <-failureCh:
		if r.OK {
			t.Fatal("expected failure result")
		}
		if r.CheckName != "failing-svc" {
			t.Fatalf("expected CheckName 'failing-svc', got %q", r.CheckName)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for OnFailure callback")
	}

	cancel()
}
