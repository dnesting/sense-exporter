package main

import (
	"context"
	_ "embed"
	"flag"
	"log"
	"net/http"
	_ "net/http/pprof"
	"time"

	"github.com/dnesting/sense"
	exporter "github.com/dnesting/sense-exporter"
	"github.com/dnesting/sense/sensecli"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// auth
	/*
		flagEmail        = flag.String("email", "", "sense.com email address")
		flagPassword     = flag.String("password", "", "sense.com password")
		flagPasswordFrom = flag.String("password-from", "", "read sense.com password from file")
		flagMfaFrom      = flag.String("mfa-from", "", "read sense.com MFA code from file")
		flagMfaCommand   = flag.String("mfa-command", "", "run command to get sense.com MFA code")
	*/

	// config
	flagAddr    = flag.String("listen", ":9553", "listen address for HTTP server")
	flagDebug   = flag.Bool("debug", false, "enable debugging")
	flagTimeout = flag.Duration("timeout", 10*time.Second, "timeout for a collection")
)

var (
	flagVersion        = flag.Bool("version", false, "print version and exit")
	Version     string = "unset"
	BuildDate   string = "unset"
)

//go:embed index.html
var indexContent []byte

func main() {
	configFile, creds := sensecli.SetupStandardFlags()
	flag.Parse()
	log.Printf("sense-exporter %s built %s\n", Version, BuildDate)
	if *flagVersion {
		return
	}

	httpClient := http.DefaultClient
	log.SetFlags(log.LstdFlags | log.Lshortfile | log.Lmicroseconds)
	if *flagDebug {
		// enable HTTP client logging
		httpClient = sense.SetDebug(log.Default(), httpClient)
	}

	ctx := context.Background()
	reg := prometheus.NewPedanticRegistry()

	cls, err := sensecli.CreateClients(ctx, configFile, creds, sense.WithHttpClient(httpClient))
	if err != nil {
		log.Fatal(err)
	}
	for _, cl := range cls {
		log.Printf("successfully authenticated account %d (monitors %v)", cl.AccountID, cl.Monitors)
		exporter.RegisterAll(ctx, cl, *flagTimeout, reg)
	}

	// Add the standard process and Go metrics to the custom registry.
	reg.MustRegister(
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		collectors.NewGoCollector(),
	)

	http.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write(indexContent)
	})
	log.Println("listening on", *flagAddr)
	log.Fatal(http.ListenAndServe(*flagAddr, nil))
}
