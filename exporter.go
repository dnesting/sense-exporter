package exporter

import (
	"context"
	"log"
	"strconv"
	"time"

	"github.com/dnesting/sense"
	"github.com/dnesting/sense/realtime"
	"github.com/prometheus/client_golang/prometheus"
)

/*
type Client interface {
	GetDevices(ctx context.Context, monitor int, includeMerged bool) ([]sense.Device, error)
	Stream(ctx context.Context, monitor int, callback realtime.Callback) error
	GetMonitors(ctx context.Context) ([]sense.Monitor, error)
}
*/

type Collector struct {
	Ctx     context.Context
	Client  *sense.Client
	Monitor int
	Timeout time.Duration
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

func (e *Collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- upDesc
	ch <- scrapeTimeDesc
	ch <- deviceWattsDesc
	ch <- voltsDesc
	ch <- wattsDesc
	ch <- hzDesc
	ch <- activeDesc
	ch <- onlineDesc
}

func labelsForDevice(d sense.Device) []string {
	return []string{d.Name, d.Type, d.Make}
}

func (e *Collector) Collect(ch chan<- prometheus.Metric) {
	log.Println("collecting from monitor", e.Monitor)
	ctx := e.Ctx
	if e.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.Timeout)
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
		log.Printf("collection for monitor %d completed in %s", e.Monitor, scrapeTime)
		ch <- prometheus.MustNewConstMetric(
			scrapeTimeDesc,
			prometheus.GaugeValue,
			scrapeSecs,
		)
	}()

	devices, err := e.Client.GetDevices(ctx, e.Monitor, false)
	if err != nil {
		log.Println(err)
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
	err = e.Client.Stream(ctx, e.Monitor, cb.callback)
	if err != nil {
		log.Println(err)
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

func Register(ctx context.Context, cl *sense.Client, monitor int, timeout time.Duration, reg prometheus.Registerer) *Collector {
	reg = prometheus.WrapRegistererWith(
		prometheus.Labels{"monitor": strconv.Itoa(monitor)},
		reg)
	col := &Collector{
		Ctx:     ctx,
		Client:  cl,
		Monitor: monitor,
		Timeout: timeout,
	}
	reg.MustRegister(col)
	return col
}

func RegisterAll(ctx context.Context, cl *sense.Client, timeout time.Duration, reg prometheus.Registerer) error {
	for _, m := range cl.Monitors {
		Register(ctx, cl, m.ID, timeout, reg)
	}
	return nil
}
