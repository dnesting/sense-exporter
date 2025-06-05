package exporter_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dnesting/sense"
	"github.com/dnesting/sense-exporter"
)

func TestNewExporter(t *testing.T) {
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
			name:    "empty clients",
			clients: []*sense.Client{},
			timeout: 5 * time.Second,
			wantNil: false,
		},
		{
			name:    "zero timeout",
			clients: []*sense.Client{},
			timeout: 0,
			wantNil: false,
		},
		{
			name:    "negative timeout",
			clients: []*sense.Client{},
			timeout: -1 * time.Second,
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
	// Create a client with a mock monitor for testing
	clientWithMonitor := sense.New()
	clientWithMonitor.Monitors = []sense.Monitor{
		{ID: 12345, SerialNumber: "test-serial"},
	}

	tests := []struct {
		name           string
		clients        []*sense.Client
		timeout        time.Duration
		wantCode       int
		wantEmptyBody  bool
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
			name:          "client with monitor",
			clients:       []*sense.Client{clientWithMonitor},
			timeout:       5 * time.Second,
			wantCode:      http.StatusOK,
			wantEmptyBody: false, // Should have metrics output
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
		containsSubstring(body, "# HELP") ||
		containsSubstring(body, "# TYPE") ||
		containsSubstring(body, "go_") ||
		containsSubstring(body, "process_"))
}

// containsSubstring is a simple helper to check if a string contains a substring
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr)
}

// findSubstring manually finds substring to avoid importing strings package
func findSubstring(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(substr) > len(s) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		found := true
		for j := 0; j < len(substr); j++ {
			if s[i+j] != substr[j] {
				found = false
				break
			}
		}
		if found {
			return true
		}
	}
	return false
}