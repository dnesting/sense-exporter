package exporter_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dnesting/sense"
	"github.com/dnesting/sense/realtime"
	exporter "github.com/dnesting/sense-exporter"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// mockClient implements the exporter.Client interface for testing
type mockClient struct {
	userID      int
	accountID   int
	monitors    []sense.Monitor
	devices     []sense.Device
	devicesErr  error
	streamErr   error
	streamCalls []streamCall
}

type streamCall struct {
	ctx      context.Context
	monitor  int
	callback realtime.Callback
}

func (m *mockClient) GetUserID() int {
	return m.userID
}

func (m *mockClient) GetAccountID() int {
	return m.accountID
}

func (m *mockClient) GetDevices(ctx context.Context, monitor int, includeMerged bool) ([]sense.Device, error) {
	if m.devicesErr != nil {
		return nil, m.devicesErr
	}
	return append([]sense.Device{}, m.devices...), nil
}

func (m *mockClient) Stream(ctx context.Context, monitor int, callback realtime.Callback) error {
	m.streamCalls = append(m.streamCalls, streamCall{
		ctx:      ctx,
		monitor:  monitor,
		callback: callback,
	})
	
	if m.streamErr != nil {
		return m.streamErr
	}
	
	// Simulate successful streaming with mock data
	realtimeUpdate := &realtime.RealtimeUpdate{
		W: 100,
		Hz: 60,
		Voltage: []float32{120.5, 119.8},
		Devices: []realtime.Device{
			{ID: "device1", W: 50},
			{ID: "device2", W: 30},
		},
	}
	
	deviceStates := &realtime.DeviceStates{
		States: []realtime.DeviceState{
			{DeviceID: "device1", Mode: "active", State: "online"},
			{DeviceID: "device2", Mode: "inactive", State: "offline"},
		},
	}
	
	// Call the callback with mock data
	if err := callback(ctx, realtimeUpdate); err != nil {
		if err == realtime.Stop {
			return nil // Stop is expected, not an error
		}
		return err
	}
	
	if err := callback(ctx, deviceStates); err != nil {
		if err == realtime.Stop {
			return nil // Stop is expected, not an error
		}
		return err
	}
	
	return nil
}

func (m *mockClient) GetMonitors() []sense.Monitor {
	return append([]sense.Monitor{}, m.monitors...)
}

func TestNewCollector(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		client    exporter.Client
		monitorID int
		timeout   time.Duration
	}{
		{
			name:      "creates collector with valid parameters",
			ctx:       context.Background(),
			client:    &mockClient{userID: 123, accountID: 456},
			monitorID: 789,
			timeout:   5 * time.Second,
		},
		{
			name:      "creates collector with zero timeout",
			ctx:       context.Background(),
			client:    &mockClient{userID: 123, accountID: 456},
			monitorID: 789,
			timeout:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector := exporter.NewCollector(tt.ctx, tt.client, tt.monitorID, tt.timeout)
			
			if collector == nil {
				t.Fatal("NewCollector returned nil")
			}
			
			// We can't directly test unexported fields, but we can test the behavior
			// by calling methods that depend on these fields
		})
	}
}

func TestCollectorDescribe(t *testing.T) {
	client := &mockClient{userID: 123, accountID: 456}
	collector := exporter.NewCollector(context.Background(), client, 789, time.Second)
	
	ch := make(chan *prometheus.Desc, 10)
	
	go func() {
		collector.Describe(ch)
		close(ch)
	}()
	
	// Collect all descriptors
	var descs []*prometheus.Desc
	for desc := range ch {
		descs = append(descs, desc)
	}
	
	// Verify we got the expected number of descriptors
	expectedCount := 8 // upDesc, scrapeTimeDesc, deviceWattsDesc, voltsDesc, wattsDesc, hzDesc, activeDesc, onlineDesc
	if len(descs) != expectedCount {
		t.Errorf("Expected %d descriptors, got %d", expectedCount, len(descs))
	}
	
	// Verify all descriptors are not nil
	for i, desc := range descs {
		if desc == nil {
			t.Errorf("Descriptor at index %d is nil", i)
		}
	}
}

func TestCollectorCollect(t *testing.T) {
	tests := []struct {
		name         string
		client       *mockClient
		expectError  bool
		expectUp     float64
		minMetrics   int // minimum number of metrics expected
	}{
		{
			name: "successful collection",
			client: &mockClient{
				userID:    123,
				accountID: 456,
				devices: []sense.Device{
					{ID: "device1", Name: "Light", Type: "Light", Make: "Philips", Model: "Hue"},
					{ID: "device2", Name: "Fridge", Type: "Refrigerator", Make: "Samsung", Model: "RF28"},
				},
			},
			expectError: false,
			expectUp:    1.0,
			minMetrics:  10, // up, scrape_time, device watts, monitor watts/volts/hz, device active/online
		},
		{
			name: "GetDevices error",
			client: &mockClient{
				userID:     123,
				accountID:  456,
				devicesErr: errors.New("failed to get devices"),
			},
			expectError: false, // Collect doesn't return error, just sets up=0
			expectUp:    0.0,
			minMetrics:  2, // up and scrape_time
		},
		{
			name: "Stream error",
			client: &mockClient{
				userID:    123,
				accountID: 456,
				devices: []sense.Device{
					{ID: "device1", Name: "Light", Type: "Light", Make: "Philips", Model: "Hue"},
				},
				streamErr: errors.New("failed to stream"),
			},
			expectError: false, // Collect doesn't return error, just sets up=0
			expectUp:    0.0,
			minMetrics:  3, // up, scrape_time, and device watts (0 value for unseen device)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector := exporter.NewCollector(context.Background(), tt.client, 789, time.Second)
			
			ch := make(chan prometheus.Metric, 50)
			
			go func() {
				collector.Collect(ch)
				close(ch)
			}()
			
			// Collect all metrics
			var metrics []prometheus.Metric
			for metric := range ch {
				metrics = append(metrics, metric)
			}
			
			if len(metrics) < tt.minMetrics {
				t.Errorf("Expected at least %d metrics, got %d", tt.minMetrics, len(metrics))
			}
			
			// Find and verify the "up" metric
			upFound := false
			scrapeTimeFound := false
			
			for _, metric := range metrics {
				dto := &dto.Metric{}
				if err := metric.Write(dto); err != nil {
					t.Errorf("Failed to write metric: %v", err)
					continue
				}
				
				// Check the metric descriptor to identify the metric type
				desc := metric.Desc()
				descStr := desc.String()
				
				if strings.Contains(descStr, `fqName: "sense_monitor_up"`) {
					upFound = true
					if dto.GetGauge() != nil {
						value := dto.GetGauge().GetValue()
						if value != tt.expectUp {
							t.Errorf("Expected up metric value %f, got %f", tt.expectUp, value)
						}
					}
				} else if strings.Contains(descStr, `fqName: "sense_scrape_time_seconds"`) {
					scrapeTimeFound = true
					// Just verify it's a gauge with a reasonable value
					if dto.GetGauge() != nil {
						value := dto.GetGauge().GetValue()
						if value < 0 {
							t.Errorf("Expected scrape time to be >= 0, got %f", value)
						}
					}
				}
			}
			
			if !upFound {
				t.Error("Expected to find 'sense_monitor_up' metric")
			}
			
			if !scrapeTimeFound {
				t.Error("Expected to find 'sense_scrape_time_seconds' metric")
			}
		})
	}
}

func TestNewExporter(t *testing.T) {
	tests := []struct {
		name     string
		clients  []exporter.Client
		timeout  time.Duration
		expectOk bool
	}{
		{
			name:     "creates exporter with clients",
			clients:  []exporter.Client{&mockClient{userID: 123}},
			timeout:  5 * time.Second,
			expectOk: true,
		},
		{
			name:     "creates exporter with no clients",
			clients:  []exporter.Client{},
			timeout:  10 * time.Second,
			expectOk: true,
		},
		{
			name:     "creates exporter with zero timeout",
			clients:  []exporter.Client{&mockClient{userID: 123}},
			timeout:  0,
			expectOk: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exp := exporter.NewExporter(tt.clients, tt.timeout)
			
			if (exp != nil) != tt.expectOk {
				t.Errorf("NewExporter() = %v, want %v", exp != nil, tt.expectOk)
			}
			
			if exp != nil {
				// Test that the exporter has the expected basic structure
				// We can't test unexported fields directly, but can test behavior
			}
		})
	}
}

func TestCollectorCollectMetricsContent(t *testing.T) {
	// Test that specific metrics are generated with correct values
	client := &mockClient{
		userID:    123,
		accountID: 456,
		devices: []sense.Device{
			{ID: "device1", Name: "Light", Type: "Light", Make: "Philips", Model: "Hue"},
		},
	}
	
	collector := exporter.NewCollector(context.Background(), client, 789, time.Second)
	
	ch := make(chan prometheus.Metric, 50)
	
	go func() {
		collector.Collect(ch)
		close(ch)
	}()
	
	// Collect all metrics and analyze their content
	metrics := make(map[string]*dto.Metric)
	for metric := range ch {
		desc := metric.Desc()
		descStr := desc.String()
		
		dto := &dto.Metric{}
		if err := metric.Write(dto); err != nil {
			t.Errorf("Failed to write metric: %v", err)
			continue
		}
		
		// Store metrics by their descriptor for easier testing
		if strings.Contains(descStr, `fqName: "sense_monitor_up"`) {
			metrics["up"] = dto
		} else if strings.Contains(descStr, `fqName: "sense_scrape_time_seconds"`) {
			metrics["scrape_time"] = dto
		} else if strings.Contains(descStr, `fqName: "sense_monitor_watts"`) {
			metrics["monitor_watts"] = dto
		} else if strings.Contains(descStr, `fqName: "sense_monitor_hz"`) {
			metrics["monitor_hz"] = dto
		} else if strings.Contains(descStr, `fqName: "sense_device_watts"`) {
			metrics["device_watts"] = dto
		}
	}
	
	// Verify specific metric values
	if upMetric := metrics["up"]; upMetric != nil {
		if upMetric.GetGauge().GetValue() != 1.0 {
			t.Errorf("Expected up=1.0, got %f", upMetric.GetGauge().GetValue())
		}
	} else {
		t.Error("Expected up metric not found")
	}
	
	if scrapeMetric := metrics["scrape_time"]; scrapeMetric != nil {
		if scrapeMetric.GetGauge().GetValue() < 0 {
			t.Errorf("Expected scrape_time >= 0, got %f", scrapeMetric.GetGauge().GetValue())
		}
	} else {
		t.Error("Expected scrape_time metric not found")
	}
	
	if wattsMetric := metrics["monitor_watts"]; wattsMetric != nil {
		if wattsMetric.GetGauge().GetValue() != 100.0 {
			t.Errorf("Expected monitor_watts=100.0, got %f", wattsMetric.GetGauge().GetValue())
		}
	} else {
		t.Error("Expected monitor_watts metric not found")
	}
	
	if hzMetric := metrics["monitor_hz"]; hzMetric != nil {
		if hzMetric.GetGauge().GetValue() != 60.0 {
			t.Errorf("Expected monitor_hz=60.0, got %f", hzMetric.GetGauge().GetValue())
		}
	} else {
		t.Error("Expected monitor_hz metric not found")
	}
}

func TestCollectorCollectWithTimeout(t *testing.T) {
	client := &mockClient{
		userID:    123,
		accountID: 456,
		devices: []sense.Device{
			{ID: "device1", Name: "Light", Type: "Light", Make: "Philips", Model: "Hue"},
		},
	}
	
	// Create collector with very short timeout
	collector := exporter.NewCollector(context.Background(), client, 789, 1*time.Nanosecond)
	
	ch := make(chan prometheus.Metric, 50)
	
	go func() {
		collector.Collect(ch)
		close(ch)
	}()
	
	// Collect all metrics
	var metrics []prometheus.Metric
	for metric := range ch {
		metrics = append(metrics, metric)
	}
	
	// Should still get up and scrape_time metrics even with timeout
	if len(metrics) < 2 {
		t.Errorf("Expected at least 2 metrics with timeout, got %d", len(metrics))
	}
}