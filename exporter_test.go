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

// mockDevice represents a device for easy test configuration
type mockDevice struct {
	ID     string
	Name   string  
	Type   string
	Make   string
	Model  string
	Watts  float32
	Active bool
	Online bool
}

// mockClient implements the exporter.Client interface for testing
type mockClient struct {
	userID     int
	accountID  int
	monitors   []sense.Monitor
	devices    []mockDevice
	devicesErr error
	streamErr  error
	
	// Monitor-level data
	totalWatts float32
	hz         float32
	voltages   []float32
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
	
	var devices []sense.Device
	for _, d := range m.devices {
		devices = append(devices, sense.Device{
			ID:    d.ID,
			Name:  d.Name,
			Type:  d.Type,
			Make:  d.Make,
			Model: d.Model,
		})
	}
	return devices, nil
}

func (m *mockClient) Stream(ctx context.Context, monitor int, callback realtime.Callback) error {
	if m.streamErr != nil {
		return m.streamErr
	}

	// Create realtime update with device power data
	var realtimeDevices []realtime.Device
	for _, d := range m.devices {
		realtimeDevices = append(realtimeDevices, realtime.Device{
			ID: d.ID,
			W:  d.Watts,
		})
	}

	realtimeUpdate := &realtime.RealtimeUpdate{
		W:       m.totalWatts,
		Hz:      m.hz,
		Voltage: m.voltages,
		Devices: realtimeDevices,
	}

	// Create device states
	var deviceStates []realtime.DeviceState
	for _, d := range m.devices {
		mode := "inactive"
		if d.Active {
			mode = "active"
		}
		state := "offline"
		if d.Online {
			state = "online"
		}
		deviceStates = append(deviceStates, realtime.DeviceState{
			DeviceID: d.ID,
			Mode:     mode,
			State:    state,
		})
	}

	deviceStatesMsg := &realtime.DeviceStates{
		States: deviceStates,
	}

	// Send both messages
	if err := callback(ctx, realtimeUpdate); err != nil {
		if err == realtime.Stop {
			return nil
		}
		return err
	}

	if err := callback(ctx, deviceStatesMsg); err != nil {
		if err == realtime.Stop {
			return nil
		}
		return err
	}

	return nil
}

func (m *mockClient) GetMonitors() []sense.Monitor {
	return append([]sense.Monitor{}, m.monitors...)
}

// Helper function to collect metrics from a collector
func collectMetrics(t *testing.T, collector *exporter.Collector) map[string][]*dto.Metric {
	ch := make(chan prometheus.Metric, 100)
	
	go func() {
		collector.Collect(ch)
		close(ch)
	}()

	metricsByName := make(map[string][]*dto.Metric)
	
	for metric := range ch {
		dto := &dto.Metric{}
		if err := metric.Write(dto); err != nil {
			t.Fatalf("Failed to write metric: %v", err)
		}
		
		desc := metric.Desc()
		descStr := desc.String()
		
		var metricName string
		if strings.Contains(descStr, `fqName: "sense_monitor_up"`) {
			metricName = "sense_monitor_up"
		} else if strings.Contains(descStr, `fqName: "sense_scrape_time_seconds"`) {
			metricName = "sense_scrape_time_seconds"
		} else if strings.Contains(descStr, `fqName: "sense_monitor_watts"`) {
			metricName = "sense_monitor_watts"
		} else if strings.Contains(descStr, `fqName: "sense_monitor_hz"`) {
			metricName = "sense_monitor_hz"
		} else if strings.Contains(descStr, `fqName: "sense_monitor_volts"`) {
			metricName = "sense_monitor_volts"
		} else if strings.Contains(descStr, `fqName: "sense_device_watts"`) {
			metricName = "sense_device_watts"
		} else {
			continue // Skip other metrics
		}
		
		metricsByName[metricName] = append(metricsByName[metricName], dto)
	}
	
	return metricsByName
}

// Helper function to verify that expected metrics are missing
func verifyMetricsMissing(t *testing.T, metrics map[string][]*dto.Metric, metricNames []string) {
	for _, name := range metricNames {
		if _, exists := metrics[name]; exists {
			t.Errorf("Expected metric %s to be missing, but it was present", name)
		}
	}
}

// Helper function to verify metric exists with expected value
func verifyMetricValue(t *testing.T, metrics map[string][]*dto.Metric, metricName string, expectedValue float64) {
	metricList, exists := metrics[metricName]
	if !exists || len(metricList) == 0 {
		t.Errorf("Expected metric %s not found", metricName)
		return
	}
	
	if len(metricList) > 1 {
		t.Errorf("Expected exactly one %s metric, got %d", metricName, len(metricList))
		return
	}
	
	actualValue := metricList[0].GetGauge().GetValue()
	// Use a tolerance for floating point comparison to handle float32->float64 conversion
	const tolerance = 1e-5
	if actualValue < expectedValue-tolerance || actualValue > expectedValue+tolerance {
		t.Errorf("Expected %s=%.6f, got %.6f", metricName, expectedValue, actualValue)
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

func TestCollectorServiceDown(t *testing.T) {
	// Case 1: Service is down and client reports errors
	client := &mockClient{
		userID:     123,
		accountID:  456,
		devicesErr: errors.New("service unavailable"),
	}
	
	collector := exporter.NewCollector(context.Background(), client, 789, time.Second)
	metrics := collectMetrics(t, collector)
	
	// Verify 'up' is 0
	verifyMetricValue(t, metrics, "sense_monitor_up", 0.0)
	
	// Verify scrape_time_seconds exists (always present)
	if _, exists := metrics["sense_scrape_time_seconds"]; !exists {
		t.Error("Expected sense_scrape_time_seconds metric to be present")
	}
	
	// Verify other main metrics are missing
	// TODO: These might be zero-valued rather than missing, which could be a bug
	verifyMetricsMissing(t, metrics, []string{
		"sense_monitor_watts",
		"sense_monitor_hz", 
		"sense_monitor_volts",
		"sense_device_watts",
	})
}

func TestCollectorStreamError(t *testing.T) {
	// Case 2: Service is up but streaming fails (similar to monitor not existing)
	client := &mockClient{
		userID:    123,
		accountID: 456,
		devices: []mockDevice{
			{ID: "device1", Name: "Light", Type: "Light", Make: "Philips", Model: "Hue"},
		},
		streamErr: errors.New("monitor not found"),
	}
	
	collector := exporter.NewCollector(context.Background(), client, 789, time.Second)
	metrics := collectMetrics(t, collector)
	
	// Verify 'up' is 0
	verifyMetricValue(t, metrics, "sense_monitor_up", 0.0)
	
	// Verify scrape_time_seconds exists
	if _, exists := metrics["sense_scrape_time_seconds"]; !exists {
		t.Error("Expected sense_scrape_time_seconds metric to be present")
	}
	
	// Should have device_watts with 0 value for devices that didn't get real values
	if deviceMetrics, exists := metrics["sense_device_watts"]; exists {
		if len(deviceMetrics) > 0 {
			// Should be 0 for devices that didn't stream successfully
			verifyMetricValue(t, metrics, "sense_device_watts", 0.0)
		}
	}
	
	// Other monitor metrics should be missing
	// TODO: These might be zero-valued rather than missing, which could be a bug
	verifyMetricsMissing(t, metrics, []string{
		"sense_monitor_watts",
		"sense_monitor_hz",
		"sense_monitor_volts",
	})
}

func TestCollectorZeroUsage(t *testing.T) {
	// Case 3: Service is up, monitor has data, but zero usage and no devices
	client := &mockClient{
		userID:     123,
		accountID:  456,
		devices:    []mockDevice{}, // No devices
		totalWatts: 0,
		hz:         60.0,
		voltages:   []float32{120.0, 119.5},
	}
	
	collector := exporter.NewCollector(context.Background(), client, 789, time.Second)
	metrics := collectMetrics(t, collector)
	
	// Verify 'up' is 1
	verifyMetricValue(t, metrics, "sense_monitor_up", 1.0)
	
	// Verify scrape_time_seconds exists
	if _, exists := metrics["sense_scrape_time_seconds"]; !exists {
		t.Error("Expected sense_scrape_time_seconds metric to be present")
	}
	
	// Verify monitor metrics exist and report appropriate values
	verifyMetricValue(t, metrics, "sense_monitor_watts", 0.0)
	verifyMetricValue(t, metrics, "sense_monitor_hz", 60.0)
	
	// Should have voltage metrics for each channel
	voltageMetrics, exists := metrics["sense_monitor_volts"]
	if !exists {
		t.Error("Expected sense_monitor_volts metrics to be present")
	} else if len(voltageMetrics) != 2 {
		t.Errorf("Expected 2 voltage metrics, got %d", len(voltageMetrics))
	}
	
	// No device metrics since no devices
	if _, exists := metrics["sense_device_watts"]; exists {
		t.Error("Expected no sense_device_watts metrics since no devices")
	}
}

func TestCollectorWithDevices(t *testing.T) {
	// Case 4: Service is up and we have mock devices reporting energy usage
	devices := []mockDevice{
		{ID: "light1", Name: "Living Room Light", Type: "Light", Make: "Philips", Model: "Hue", Watts: 25.5, Active: true, Online: true},
		{ID: "fridge1", Name: "Kitchen Fridge", Type: "Refrigerator", Make: "Samsung", Model: "RF28", Watts: 150.0, Active: true, Online: true},
		{ID: "washer1", Name: "Washing Machine", Type: "Washer", Make: "LG", Model: "WM3500", Watts: 0, Active: false, Online: false},
	}
	
	client := &mockClient{
		userID:     123,
		accountID:  456,
		devices:    devices,
		totalWatts: 175.5, // Sum of active device watts
		hz:         59.8,
		voltages:   []float32{121.2, 120.8},
	}
	
	collector := exporter.NewCollector(context.Background(), client, 789, time.Second)
	metrics := collectMetrics(t, collector)
	
	// Verify 'up' is 1
	verifyMetricValue(t, metrics, "sense_monitor_up", 1.0)
	
	// Verify scrape_time_seconds exists
	if _, exists := metrics["sense_scrape_time_seconds"]; !exists {
		t.Error("Expected sense_scrape_time_seconds metric to be present")
	}
	
	// Verify monitor-level metrics
	verifyMetricValue(t, metrics, "sense_monitor_watts", 175.5)
	verifyMetricValue(t, metrics, "sense_monitor_hz", 59.8)
	
	// Verify voltage metrics for each channel
	voltageMetrics, exists := metrics["sense_monitor_volts"]
	if !exists {
		t.Error("Expected sense_monitor_volts metrics to be present")
	} else if len(voltageMetrics) != 2 {
		t.Errorf("Expected 2 voltage metrics, got %d", len(voltageMetrics))
	}
	
	// Verify device watts metrics - should have one for each device
	deviceWattsMetrics, exists := metrics["sense_device_watts"]
	if !exists {
		t.Error("Expected sense_device_watts metrics to be present")
	} else {
		if len(deviceWattsMetrics) != len(devices) {
			t.Errorf("Expected %d device watts metrics, got %d", len(devices), len(deviceWattsMetrics))
		}
		
		// Verify each device has correct wattage and labels
		deviceWattsByID := make(map[string]float64)
		for _, metric := range deviceWattsMetrics {
			// Find device_id label
			var deviceID string
			for _, label := range metric.GetLabel() {
				if label.GetName() == "device_id" {
					deviceID = label.GetValue()
					break
				}
			}
			if deviceID == "" {
				t.Error("Device watts metric missing device_id label")
				continue
			}
			deviceWattsByID[deviceID] = metric.GetGauge().GetValue()
		}
		
		// Verify values match expected
		expectedWatts := map[string]float64{
			"light1":  25.5,
			"fridge1": 150.0,  
			"washer1": 0.0,
		}
		
		for deviceID, expectedWatt := range expectedWatts {
			if actualWatt, exists := deviceWattsByID[deviceID]; !exists {
				t.Errorf("Expected device %s watts metric not found", deviceID)
			} else if actualWatt != expectedWatt {
				t.Errorf("Expected device %s watts=%.1f, got %.1f", deviceID, expectedWatt, actualWatt)
			}
		}
	}
}
