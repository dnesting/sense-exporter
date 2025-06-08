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
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
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
	flagJaeger  = flag.String("jaeger", "", "jaeger endpoint (e.g. http://localhost:14268/api/traces)")
)

var (
	flagVersion = flag.Bool("version", false, "print version and exit")
)

//go:embed index.html
var indexContent []byte

const traceName = "github.com/dnesting/sense-exporter"

func main() {
	configFile, creds := sensecli.SetupStandardFlags()
	flag.Parse()
	log.Printf("sense-exporter %s built %s\n", Version, BuildDate)
	if *flagVersion {
		return
	}

	httpClient := http.DefaultClient
	ctx := context.Background()
	if *flagJaeger != "" {
		var cancel func(context.Context)
		var err error
		ctx, cancel, err = setupTracing(ctx, *flagJaeger, "sense-exporter")
		if err != nil {
			log.Fatal(err)
		}
		defer cancel(ctx)

		httpClient = &http.Client{
			Transport: otelhttp.NewTransport(httpClient.Transport),
		}
	}

	log.SetFlags(log.LstdFlags | log.Lshortfile | log.Lmicroseconds)
	if *flagDebug {
		// enable HTTP client logging
		httpClient = sense.SetDebug(log.Default(), httpClient)
	}

	ctx, span := otel.Tracer(traceName).Start(ctx, "Setup")
	cls, err := sensecli.CreateClients(ctx, configFile, creds, sense.WithHttpClient(httpClient))
	if err != nil {
		span.RecordError(err)
		log.Fatal(err)
	}
	for _, cl := range cls {
		if cl.GetAccountID() > 0 {
			log.Printf("successfully authenticated account %d (monitors %v)", cl.GetAccountID(), cl.GetMonitors())
		}
	}

	exp := exporter.NewExporter(cls, *flagTimeout)

	http.Handle("/metrics", otelhttp.NewHandler(exp, "/metrics"))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write(indexContent)
	})
	log.Println("listening on", *flagAddr)
	span.End()
	log.Fatal(http.ListenAndServe(*flagAddr, nil))
}
