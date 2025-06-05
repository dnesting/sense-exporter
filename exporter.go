package exporter

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/dnesting/sense"
	"github.com/dnesting/sense/realtime"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// Client interface abstracts the Sense client functionality
type Client interface {
	GetDevices(ctx context.Context, monitor int, includeMerged bool) ([]sense.Device, error)
	Stream(ctx context.Context, monitor int, callback realtime.Callback) error
	// Fields needed for tracing and monitoring
	GetUserID() int
	GetAccountID() int
	GetMonitors() []sense.Monitor
}

// senseClientWrapper wraps *sense.Client to implement our Client interface
type senseClientWrapper struct {
	*sense.Client
}

func (w *senseClientWrapper) GetUserID() int {
	return w.Client.UserID
}

func (w *senseClientWrapper) GetAccountID() int {
	return w.Client.AccountID
}

func (w *senseClientWrapper) GetMonitors() []sense.Monitor {
	return w.Client.Monitors
}

type Exporter struct {
	clients []Client
	timeout time.Duration
	colls   []prometheus.Collector
}

var (
	upDesc = prometheus.NewDesc("sense_monitor_up",
		"Whether a Sense monitor is online and accessible to us",
		[]string{}, nil)
	scrapeTimeDesc = prometheus.NewDesc("sense_scrape_time_seconds",
		"Time spent scraping Sense",
		[]string{}, nil)

	// RealtimeUpdate
	deviceWattsDesc = prometheus.NewDesc("sense_device_watts",
		"Current power usage of a device",
		[]string{"device_id", "name", "type", "make", "model"}, nil)
	voltsDesc = prometheus.NewDesc("sense_monitor_volts",
		"Current voltage detected by the Sense monitor",
		[]string{"channel"}, nil)
	wattsDesc = prometheus.NewDesc("sense_monitor_watts",
		"Current voltage detected by the Sense monitor",
		[]string{}, nil)
	hzDesc = prometheus.NewDesc("sense_monitor_hz",
		"Current frequency detected by the Sense monitor",
		[]string{}, nil)

	// DeviceStates States[]
	activeDesc = prometheus.NewDesc("sense_device_active",
		"Whether a Sense device is active",
		[]string{"device_id", "name", "type", "make", "model"}, nil)
	onlineDesc = prometheus.NewDesc("sense_device_online",
		"Whether a Sense device is online",
		[]string{"device_id", "name", "type", "make", "model"}, nil)
)

const traceName = "github.com/dnesting/sense-exporter"

func (e *Exporter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	reg := prometheus.NewPedanticRegistry()

	var colls []prometheus.Collector
	for _, cl := range e.clients {
		for _, m := range cl.GetMonitors() {
			c := NewCollectorWithTimeout(r.Context(), cl, m.ID, e.timeout)
			colls = append(colls, c)
			rg := prometheus.WrapRegistererWith(
				prometheus.Labels{"monitor": strconv.Itoa(m.ID)},
				reg)
			rg.MustRegister(e.colls...)
			rg.MustRegister(colls...)
		}
	}
	promhttp.HandlerFor(reg, promhttp.HandlerOpts{}).ServeHTTP(w, r)
}

type Collector struct {
	ctx     context.Context
	cl      Client
	timeout time.Duration
	monitor int
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- upDesc
	ch <- scrapeTimeDesc
	ch <- deviceWattsDesc
	ch <- voltsDesc
	ch <- wattsDesc
	ch <- hzDesc
	ch <- activeDesc
	ch <- onlineDesc
}

func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	log.Println("collecting from monitor", c.monitor)
	ctx, span := otel.Tracer(traceName).Start(c.ctx, "Collect from Sense Monitor "+strconv.Itoa(c.monitor))
	defer span.End()
	span.SetAttributes(attribute.Int("sense-userid", c.cl.GetUserID()))
	span.SetAttributes(attribute.Int("sense-account", c.cl.GetAccountID()))
	span.SetAttributes(attribute.Int("sense-monitor", c.monitor))
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}
	start := time.Now()
	collectOk := 1.0
	defer func() {
		ch <- prometheus.MustNewConstMetric(
			upDesc,
			prometheus.GaugeValue,
			collectOk,
		)

		scrapeTime := time.Since(start)
		scrapeSecs := scrapeTime.Seconds()
		log.Printf("collection for monitor %d completed in %s", c.monitor, scrapeTime)
		ch <- prometheus.MustNewConstMetric(
			scrapeTimeDesc,
			prometheus.GaugeValue,
			scrapeSecs,
		)
	}()

	devices, err := c.cl.GetDevices(ctx, c.monitor, false)
	if err != nil {
		log.Println(err)
		span.RecordError(err)
		collectOk = 0
		return
	}

	// Collect basic information about devices. We'll use these in labels later.
	devInfo := make(map[string]sense.Device)
	for _, d := range devices {
		devInfo[d.ID] = d
	}

	cb := &callbackContainer{
		ch:        ch,
		devInfo:   devInfo,
		seenWatts: make(map[string]bool),
	}
	err = c.cl.Stream(ctx, c.monitor, cb.callback)
	if err != nil {
		log.Println(err)
		span.RecordError(err)
		collectOk = 0
	}

	for _, d := range devices {
		if !cb.seenWatts[d.ID] {
			ch <- prometheus.MustNewConstMetric(
				deviceWattsDesc,
				prometheus.GaugeValue,
				0,
				d.ID,
				devInfo[d.ID].Name,
				devInfo[d.ID].Type,
				devInfo[d.ID].Make,
				devInfo[d.ID].Model,
			)
		}
	}
}

type callbackContainer struct {
	gotRealtime bool
	gotStates   bool
	ch          chan<- prometheus.Metric
	devInfo     map[string]sense.Device
	seenWatts   map[string]bool
}

func (e *callbackContainer) callback(ctx context.Context, msg realtime.Message) error {
	switch msg := msg.(type) {

	case *realtime.RealtimeUpdate:
		if e.gotRealtime {
			return nil
		}
		for _, d := range msg.Devices {
			e.ch <- prometheus.MustNewConstMetric(
				deviceWattsDesc,
				prometheus.GaugeValue,
				float64(d.W),
				d.ID,
				e.devInfo[d.ID].Name,
				e.devInfo[d.ID].Type,
				e.devInfo[d.ID].Make,
				e.devInfo[d.ID].Model,
			)
			e.seenWatts[d.ID] = true
		}
		for channel, v := range msg.Voltage {
			e.ch <- prometheus.MustNewConstMetric(
				voltsDesc,
				prometheus.GaugeValue,
				float64(v),
				strconv.Itoa(channel),
			)
		}
		e.ch <- prometheus.MustNewConstMetric(
			wattsDesc,
			prometheus.GaugeValue,
			float64(msg.W),
		)
		e.ch <- prometheus.MustNewConstMetric(
			hzDesc,
			prometheus.GaugeValue,
			float64(msg.Hz),
		)
		e.gotRealtime = true

	case *realtime.DeviceStates:
		if e.gotStates {
			return nil
		}
		for _, d := range msg.States {
			var active, online float64
			if d.Mode == "active" {
				active = 1.0
			}
			if d.State == "online" {
				online = 1.0
			}
			e.ch <- prometheus.MustNewConstMetric(
				activeDesc,
				prometheus.GaugeValue,
				active,
				d.DeviceID,
				e.devInfo[d.DeviceID].Name,
				e.devInfo[d.DeviceID].Type,
				e.devInfo[d.DeviceID].Make,
				e.devInfo[d.DeviceID].Model,
			)
			e.ch <- prometheus.MustNewConstMetric(
				onlineDesc,
				prometheus.GaugeValue,
				online,
				d.DeviceID,
				e.devInfo[d.DeviceID].Name,
				e.devInfo[d.DeviceID].Type,
				e.devInfo[d.DeviceID].Make,
				e.devInfo[d.DeviceID].Model,
			)
		}
		e.gotStates = true
	}

	if e.gotRealtime && e.gotStates {
		return realtime.Stop
	}
	return nil
}

// NewCollector creates a new Collector for the specified client and monitor
func NewCollector(ctx context.Context, client Client, monitor int) *Collector {
	return &Collector{
		ctx:     ctx,
		cl:      client,
		timeout: 0, // Default timeout, can be set later
		monitor: monitor,
	}
}

// NewCollectorWithTimeout creates a new Collector with a specific timeout
func NewCollectorWithTimeout(ctx context.Context, client Client, monitor int, timeout time.Duration) *Collector {
	return &Collector{
		ctx:     ctx,
		cl:      client,
		timeout: timeout,
		monitor: monitor,
	}
}

func NewExporter(clients []*sense.Client, timeout time.Duration) *Exporter {
	// Convert sense.Client to our Client interface
	wrappedClients := make([]Client, len(clients))
	for i, cl := range clients {
		wrappedClients[i] = &senseClientWrapper{Client: cl}
	}
	
	e := &Exporter{
		clients: wrappedClients,
		timeout: timeout,
		colls: []prometheus.Collector{
			collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
			collectors.NewGoCollector(),
		},
	}
	return e
}
