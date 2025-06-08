package exporter_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dnesting/sense"
	exporter "github.com/dnesting/sense-exporter"
	"github.com/dnesting/sense/realtime"
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
		W:       100,
		Hz:      60,
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
		name                string
		client              *mockClient
		expectError         bool
		expectUp            float64
		minMetrics          int  // minimum number of metrics expected
		checkDetailedValues bool // whether to check detailed metric values
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
			expectError:         false,
			expectUp:            1.0,
			minMetrics:          10, // up, scrape_time, device watts, monitor watts/volts/hz, device active/online
			checkDetailedValues: true,
		},
		{
			name: "GetDevices error",
			client: &mockClient{
				userID:     123,
				accountID:  456,
				devicesErr: errors.New("failed to get devices"),
			},
			expectError:         false, // Collect doesn't return error, just sets up=0
			expectUp:            0.0,
			minMetrics:          2, // up and scrape_time
			checkDetailedValues: false,
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
			expectError:         false, // Collect doesn't return error, just sets up=0
			expectUp:            0.0,
			minMetrics:          3, // up, scrape_time, and device watts (0 value for unseen device)
			checkDetailedValues: false,
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

			// Collect all metrics and organize them for testing
			var metrics []prometheus.Metric
			metricsByName := make(map[string]*dto.Metric)

			for metric := range ch {
				metrics = append(metrics, metric)

				// Parse metric for detailed checking
				dto := &dto.Metric{}
				if err := metric.Write(dto); err != nil {
					t.Errorf("Failed to write metric: %v", err)
					continue
				}

				// Store metrics by their descriptor for easier testing
				desc := metric.Desc()
				descStr := desc.String()

				if strings.Contains(descStr, `fqName: "sense_monitor_up"`) {
					metricsByName["up"] = dto
				} else if strings.Contains(descStr, `fqName: "sense_scrape_time_seconds"`) {
					metricsByName["scrape_time"] = dto
				} else if strings.Contains(descStr, `fqName: "sense_monitor_watts"`) {
					metricsByName["monitor_watts"] = dto
				} else if strings.Contains(descStr, `fqName: "sense_monitor_hz"`) {
					metricsByName["monitor_hz"] = dto
				} else if strings.Contains(descStr, `fqName: "sense_device_watts"`) {
					metricsByName["device_watts"] = dto
				}
			}

			if len(metrics) < tt.minMetrics {
				t.Errorf("Expected at least %d metrics, got %d", tt.minMetrics, len(metrics))
			}

			// Verify the "up" metric
			if upMetric := metricsByName["up"]; upMetric != nil {
				if upMetric.GetGauge().GetValue() != tt.expectUp {
					t.Errorf("Expected up metric value %f, got %f", tt.expectUp, upMetric.GetGauge().GetValue())
				}
			} else {
				t.Error("Expected to find 'sense_monitor_up' metric")
			}

			// Verify scrape time metric
			if scrapeMetric := metricsByName["scrape_time"]; scrapeMetric != nil {
				if scrapeMetric.GetGauge().GetValue() < 0 {
					t.Errorf("Expected scrape time to be >= 0, got %f", scrapeMetric.GetGauge().GetValue())
				}
			} else {
				t.Error("Expected to find 'sense_scrape_time_seconds' metric")
			}

			// For successful collection, verify detailed metric values
			if tt.checkDetailedValues {
				if wattsMetric := metricsByName["monitor_watts"]; wattsMetric != nil {
					if wattsMetric.GetGauge().GetValue() != 100.0 {
						t.Errorf("Expected monitor_watts=100.0, got %f", wattsMetric.GetGauge().GetValue())
					}
				} else {
					t.Error("Expected monitor_watts metric not found")
				}

				if hzMetric := metricsByName["monitor_hz"]; hzMetric != nil {
					if hzMetric.GetGauge().GetValue() != 60.0 {
						t.Errorf("Expected monitor_hz=60.0, got %f", hzMetric.GetGauge().GetValue())
					}
				} else {
					t.Error("Expected monitor_hz metric not found")
				}
			}
		})
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
