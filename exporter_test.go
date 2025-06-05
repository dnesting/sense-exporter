package exporter_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dnesting/sense"
	"github.com/dnesting/sense-exporter"
)

func TestNewExporter(t *testing.T) {
	// Create some test clients for testing
	client1 := sense.New()
	client1.Monitors = []sense.Monitor{{ID: 1, SerialNumber: "test-1"}}
	
	client2 := sense.New()
	client2.Monitors = []sense.Monitor{
		{ID: 2, SerialNumber: "test-2"},
		{ID: 3, SerialNumber: "test-3"},
	}

	tests := []struct {
		name     string
		clients  []*sense.Client
		timeout  time.Duration
		wantNil  bool
	}{
		{
			name:    "nil clients",
			clients: nil,
			timeout: 10 * time.Second,
			wantNil: false,
		},
		{
			name:    "empty clients slice",
			clients: []*sense.Client{},
			timeout: 5 * time.Second,
			wantNil: false,
		},
		{
			name:    "single client",
			clients: []*sense.Client{client1},
			timeout: 5 * time.Second,
			wantNil: false,
		},
		{
			name:    "multiple clients",
			clients: []*sense.Client{client1, client2},
			timeout: 5 * time.Second,
			wantNil: false,
		},
		{
			name:    "zero timeout",
			clients: []*sense.Client{client1},
			timeout: 0,
			wantNil: false,
		},
		{
			name:    "negative timeout",
			clients: []*sense.Client{client1},
			timeout: -1 * time.Second,
			wantNil: false,
		},
		{
			name:    "very long timeout",
			clients: []*sense.Client{client1},
			timeout: 24 * time.Hour,
			wantNil: false,
		},
		{
			name:    "minimal timeout",
			clients: []*sense.Client{client1},
			timeout: 1 * time.Nanosecond,
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := exporter.NewExporter(tt.clients, tt.timeout)
			
			if tt.wantNil && got != nil {
				t.Errorf("NewExporter() = %v, want nil", got)
				return
			}
			
			if !tt.wantNil && got == nil {
				t.Errorf("NewExporter() = nil, want non-nil")
				return
			}
			
			if got == nil {
				return // Skip further checks if got is nil
			}
			
			// We can't directly access the fields since they're unexported,
			// but we can verify that NewExporter returns a non-nil *Exporter
			// This is the limit of what we can test with the _test package approach
		})
	}
}

func TestExporter_ServeHTTP(t *testing.T) {
	// Create clients with different monitor configurations
	clientWithMonitor := sense.New()
	clientWithMonitor.Monitors = []sense.Monitor{
		{ID: 12345, SerialNumber: "test-serial-1"},
	}

	clientWithNoMonitors := sense.New()
	clientWithNoMonitors.Monitors = []sense.Monitor{} // explicitly empty

	tests := []struct {
		name           string
		clients        []*sense.Client
		timeout        time.Duration
		wantCode       int
		wantEmptyBody  bool
		wantPanic      bool // Some configurations cause panics due to duplicate registration
	}{
		{
			name:          "no clients",
			clients:       []*sense.Client{},
			timeout:       5 * time.Second,
			wantCode:      http.StatusOK,
			wantEmptyBody: true, // No collectors registered when no monitors
		},
		{
			name:          "nil clients",
			clients:       nil,
			timeout:       5 * time.Second,
			wantCode:      http.StatusOK,
			wantEmptyBody: true, // No collectors registered when no monitors
		},
		{
			name:          "client with single monitor",
			clients:       []*sense.Client{clientWithMonitor},
			timeout:       5 * time.Second,
			wantCode:      http.StatusOK,
			wantEmptyBody: false, // Should have metrics output
		},
		{
			name:          "client with no monitors",
			clients:       []*sense.Client{clientWithNoMonitors},
			timeout:       5 * time.Second,
			wantCode:      http.StatusOK,
			wantEmptyBody: true, // No monitors means no collectors registered
		},
		{
			name:          "mixed clients - some with monitors, some without",
			clients:       []*sense.Client{clientWithNoMonitors, clientWithMonitor},
			timeout:       5 * time.Second,
			wantCode:      http.StatusOK,
			wantEmptyBody: false, // Should have metrics from the client with monitors
		},
		{
			name:          "zero timeout",
			clients:       []*sense.Client{clientWithMonitor},
			timeout:       0, // No timeout
			wantCode:      http.StatusOK,
			wantEmptyBody: false, // Should still work
		},
		{
			name:          "short timeout",
			clients:       []*sense.Client{clientWithMonitor},
			timeout:       1 * time.Millisecond, // Very short timeout
			wantCode:      http.StatusOK,
			wantEmptyBody: false, // Should still work (may timeout but still produce some metrics)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantPanic {
				defer func() {
					if r := recover(); r == nil {
						t.Errorf("Expected panic but didn't get one")
					}
				}()
			}

			exporter := exporter.NewExporter(tt.clients, tt.timeout)
			
			req := httptest.NewRequest("GET", "/metrics", nil)
			w := httptest.NewRecorder()
			
			exporter.ServeHTTP(w, req)
			
			if got := w.Code; got != tt.wantCode {
				t.Errorf("ServeHTTP() status code = %v, want %v", got, tt.wantCode)
			}
			
			body := w.Body.String()
			isEmpty := body == ""
			
			if tt.wantEmptyBody && !isEmpty {
				t.Errorf("ServeHTTP() expected empty body, got: %s", body)
			}
			
			if !tt.wantEmptyBody && isEmpty {
				t.Error("ServeHTTP() returned empty body, expected metrics output")
			}
			
			// For non-empty responses, check for prometheus format
			if !tt.wantEmptyBody && !isEmpty {
				// Should contain some basic prometheus metrics from default collectors
				if !containsPrometheusMetrics(body) {
					t.Errorf("ServeHTTP() response doesn't look like prometheus metrics: %s", body)
				}
			}
		})
	}
}

// containsPrometheusMetrics checks if the response contains prometheus-formatted metrics
func containsPrometheusMetrics(body string) bool {
	// Look for common prometheus patterns like HELP or TYPE comments
	return body != "" && (
		strings.Contains(body, "# HELP") ||
		strings.Contains(body, "# TYPE") ||
		strings.Contains(body, "go_") ||
		strings.Contains(body, "process_"))
}

func TestExporter_ServeHTTP_EdgeCases(t *testing.T) {
	clientWithMonitor := sense.New()
	clientWithMonitor.Monitors = []sense.Monitor{
		{ID: 99999, SerialNumber: "edge-case-test"},
	}

	tests := []struct {
		name      string
		method    string
		path      string
		clients   []*sense.Client
		timeout   time.Duration
		wantCode  int
	}{
		{
			name:     "GET request",
			method:   "GET",
			path:     "/metrics",
			clients:  []*sense.Client{clientWithMonitor},
			timeout:  5 * time.Second,
			wantCode: http.StatusOK,
		},
		{
			name:     "POST request (should still work)",
			method:   "POST",
			path:     "/metrics",
			clients:  []*sense.Client{clientWithMonitor},
			timeout:  5 * time.Second,
			wantCode: http.StatusOK,
		},
		{
			name:     "PUT request (should still work)",
			method:   "PUT",
			path:     "/metrics",
			clients:  []*sense.Client{clientWithMonitor},
			timeout:  5 * time.Second,
			wantCode: http.StatusOK,
		},
		{
			name:     "different path (should still work - prometheus handler doesn't care about path)",
			method:   "GET",
			path:     "/different",
			clients:  []*sense.Client{clientWithMonitor},
			timeout:  5 * time.Second,
			wantCode: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exporter := exporter.NewExporter(tt.clients, tt.timeout)
			
			req := httptest.NewRequest(tt.method, tt.path, nil)
			w := httptest.NewRecorder()
			
			exporter.ServeHTTP(w, req)
			
			if got := w.Code; got != tt.wantCode {
				t.Errorf("ServeHTTP() status code = %v, want %v", got, tt.wantCode)
			}
		})
	}
}