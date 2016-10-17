package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"golang.org/x/net/context"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/metrics/pcp"

	"github.com/go-kit/kit/examples/shipping/booking"
	"github.com/go-kit/kit/examples/shipping/cargo"
	"github.com/go-kit/kit/examples/shipping/handling"
	"github.com/go-kit/kit/examples/shipping/inspection"
	"github.com/go-kit/kit/examples/shipping/location"
	"github.com/go-kit/kit/examples/shipping/repository"
	"github.com/go-kit/kit/examples/shipping/routing"
	"github.com/go-kit/kit/examples/shipping/tracking"
)

const (
	defaultPort              = "8080"
	defaultRoutingServiceURL = "http://localhost:7878"
)

func main() {
	var (
		addr  = envString("PORT", defaultPort)
		rsurl = envString("ROUTINGSERVICE_URL", defaultRoutingServiceURL)

		httpAddr          = flag.String("http.addr", ":"+addr, "HTTP listen address")
		routingServiceURL = flag.String("service.routing", rsurl, "routing service URL")

		ctx = context.Background()
	)

	flag.Parse()

	var logger log.Logger
	logger = log.NewLogfmtLogger(os.Stderr)
	logger = &serializedLogger{Logger: logger}
	logger = log.NewContext(logger).With("ts", log.DefaultTimestampUTC)

	var (
		cargos         = repository.NewCargo()
		locations      = repository.NewLocation()
		voyages        = repository.NewVoyage()
		handlingEvents = repository.NewHandlingEvent()
	)

	// Configure some questionable dependencies.
	var (
		handlingEventFactory = cargo.HandlingEventFactory{
			CargoRepository:    cargos,
			VoyageRepository:   voyages,
			LocationRepository: locations,
		}
		handlingEventHandler = handling.NewEventHandler(
			inspection.NewService(cargos, handlingEvents, nil),
		)
	)

	// Facilitate testing by adding some cargos.
	storeTestData(cargos)

	// fieldKeys := []string{"method"}

	var rs routing.Service
	rs = routing.NewProxyingMiddleware(*routingServiceURL, ctx)(rs)

	var bs booking.Service
	bs = booking.NewService(cargos, locations, handlingEvents, rs)
	bs = booking.NewLoggingService(log.NewContext(logger).With("component", "booking"), bs)
	bs = booking.NewInstrumentingService(
		pcp.NewCounter("api.booking_service.request_count", "Number of requests received."),
		pcp.NewHistogram("api.booking_service.request_latency_microseconds", "Total duration of requests in microseconds."),
		bs,
	)

	var ts tracking.Service
	ts = tracking.NewService(cargos, handlingEvents)
	ts = tracking.NewLoggingService(log.NewContext(logger).With("component", "tracking"), ts)
	ts = tracking.NewInstrumentingService(
		pcp.NewCounter("api.tracking_service.request_count", "Number of requests received."),
		pcp.NewHistogram("api.tracking_service.request_latency_microseconds", "Total duration of requests in microseconds."),
		ts,
	)

	var hs handling.Service
	hs = handling.NewService(handlingEvents, handlingEventFactory, handlingEventHandler)
	hs = handling.NewLoggingService(log.NewContext(logger).With("component", "handling"), hs)
	hs = handling.NewInstrumentingService(
		pcp.NewCounter("api.handling_service.request_count", "Number of requests received."),
		pcp.NewHistogram("api.handling_service.request_latency_microseconds", "Total duration of requests in microseconds."),
		hs,
	)

	httpLogger := log.NewContext(logger).With("component", "http")

	mux := http.NewServeMux()

	mux.Handle("/booking/v1/", booking.MakeHandler(ctx, bs, httpLogger))
	mux.Handle("/tracking/v1/", tracking.MakeHandler(ctx, ts, httpLogger))
	mux.Handle("/handling/v1/", handling.MakeHandler(ctx, hs, httpLogger))

	http.Handle("/", accessControl(mux))

	pcp.StartReporting("shipping")
	defer pcp.StopReporting()

	errs := make(chan error, 2)
	go func() {
		logger.Log("transport", "http", "address", *httpAddr, "msg", "listening")
		errs <- http.ListenAndServe(*httpAddr, nil)
	}()
	go func() {
		c := make(chan os.Signal)
		signal.Notify(c, syscall.SIGINT)
		errs <- fmt.Errorf("%s", <-c)
	}()

	logger.Log("terminated", <-errs)
}

func accessControl(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type")

		if r.Method == "OPTIONS" {
			return
		}

		h.ServeHTTP(w, r)
	})
}

func envString(env, fallback string) string {
	e := os.Getenv(env)
	if e == "" {
		return fallback
	}
	return e
}

func storeTestData(r cargo.Repository) {
	test1 := cargo.New("FTL456", cargo.RouteSpecification{
		Origin:          location.AUMEL,
		Destination:     location.SESTO,
		ArrivalDeadline: time.Now().AddDate(0, 0, 7),
	})
	_ = r.Store(test1)

	test2 := cargo.New("ABC123", cargo.RouteSpecification{
		Origin:          location.SESTO,
		Destination:     location.CNHKG,
		ArrivalDeadline: time.Now().AddDate(0, 0, 14),
	})
	_ = r.Store(test2)
}

type serializedLogger struct {
	mtx sync.Mutex
	log.Logger
}

func (l *serializedLogger) Log(keyvals ...interface{}) error {
	l.mtx.Lock()
	defer l.mtx.Unlock()
	return l.Logger.Log(keyvals...)
}
