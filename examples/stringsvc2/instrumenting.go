package main

import (
	"fmt"
	"time"

	"github.com/go-kit/kit/metrics"
)

func toMicroSecond(nanoseconds int64) float64 {
	return float64(nanoseconds) * 1e-3
}

type instrumentingMiddleware struct {
	requestCount   metrics.Counter
	requestLatency metrics.Histogram
	countResult    metrics.Histogram
	next           StringService
}

func (mw instrumentingMiddleware) Uppercase(s string) (output string, err error) {
	defer func(begin time.Time) {
		lvs := []string{"method", "uppercase", "error", fmt.Sprint(err != nil)}
		mw.requestCount.With(lvs...).Add(1)
		mw.requestLatency.With(lvs...).Observe(toMicroSecond(time.Since(begin).Nanoseconds()))
	}(time.Now())

	output, err = mw.next.Uppercase(s)
	return
}

func (mw instrumentingMiddleware) Count(s string) (n int) {
	defer func(begin time.Time) {
		lvs := []string{"method", "count", "error", "false"}
		mw.requestCount.With(lvs...).Add(1)
		mw.requestLatency.With(lvs...).Observe(toMicroSecond(time.Since(begin).Nanoseconds()))
		mw.countResult.Observe(float64(n))
	}(time.Now())

	n = mw.next.Count(s)
	return
}
