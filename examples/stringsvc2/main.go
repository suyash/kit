package main

import (
	"net/http"
	"os"

	"golang.org/x/net/context"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/metrics/pcp"
	httptransport "github.com/go-kit/kit/transport/http"
)

func main() {
	ctx := context.Background()
	logger := log.NewLogfmtLogger(os.Stderr)

	// fieldKeys := []string{"method", "error"}
	requestCount := pcp.NewCounter("request.count")
	requestLatency := pcp.NewHistogram("request.latency")
	countResult := pcp.NewHistogram("count.values")

	var svc StringService
	svc = stringService{}
	svc = loggingMiddleware{logger, svc}
	svc = instrumentingMiddleware{requestCount, requestLatency, countResult, svc}

	uppercaseHandler := httptransport.NewServer(
		ctx,
		makeUppercaseEndpoint(svc),
		decodeUppercaseRequest,
		encodeResponse,
	)

	countHandler := httptransport.NewServer(
		ctx,
		makeCountEndpoint(svc),
		decodeCountRequest,
		encodeResponse,
	)

	http.Handle("/uppercase", uppercaseHandler)
	http.Handle("/count", countHandler)
	// http.Handle("/metrics", stdprometheus.Handler())

	pcp.StartReporting("stringsvc")
	defer pcp.StopReporting()

	logger.Log("msg", "HTTP", "addr", ":8080")
	logger.Log("err", http.ListenAndServe(":8080", nil))
}
