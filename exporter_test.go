package exporter

import (
	"context"
	"testing"
	"time"

	"github.com/dnesting/sense"
	"github.com/dnesting/sense/realtime"
)

// mockClient implements the Client interface for testing
type mockClient struct {
	userID    int
	accountID int
	monitors  []sense.Monitor
}

func (m *mockClient) GetUserID() int {
	return m.userID
}

func (m *mockClient) GetAccountID() int {
	return m.accountID
}

func (m *mockClient) GetDevices(ctx context.Context, monitor int, includeMerged bool) ([]sense.Device, error) {
	return []sense.Device{}, nil
}

func (m *mockClient) Stream(ctx context.Context, monitor int, callback realtime.Callback) error {
	return nil
}

func (m *mockClient) GetMonitors() []sense.Monitor {
	return m.monitors
}

func TestNewCollector(t *testing.T) {
	ctx := context.Background()
	client := &mockClient{
		userID:    123,
		accountID: 456,
		monitors:  []sense.Monitor{{ID: 1}},
	}
	timeout := 10 * time.Second
	monitorID := 1

	collector := NewCollector(ctx, client, monitorID, timeout)

	if collector == nil {
		t.Fatal("NewCollector returned nil")
	}

	if collector.ctx != ctx {
		t.Errorf("Expected context %v, got %v", ctx, collector.ctx)
	}

	if collector.cl != client {
		t.Errorf("Expected client %v, got %v", client, collector.cl)
	}

	if collector.timeout != timeout {
		t.Errorf("Expected timeout %v, got %v", timeout, collector.timeout)
	}

	if collector.monitor != monitorID {
		t.Errorf("Expected monitor ID %d, got %d", monitorID, collector.monitor)
	}
}

func TestNewExporter(t *testing.T) {
	clients := []Client{
		&mockClient{userID: 123, accountID: 456},
		&mockClient{userID: 789, accountID: 012},
	}
	timeout := 15 * time.Second

	exporter := NewExporter(clients, timeout)

	if exporter == nil {
		t.Fatal("NewExporter returned nil")
	}

	if len(exporter.clients) != len(clients) {
		t.Errorf("Expected %d clients, got %d", len(clients), len(exporter.clients))
	}

	if exporter.timeout != timeout {
		t.Errorf("Expected timeout %v, got %v", timeout, exporter.timeout)
	}

	if len(exporter.colls) == 0 {
		t.Error("Expected some collectors to be initialized")
	}
}

func TestClientInterface(t *testing.T) {
	// Test that *sense.Client implements Client interface
	var _ Client = (*sense.Client)(nil)
}