// Copyright (c) 2015-2023 MinIO, Inc.
//
// This file is part of MinIO Object Storage stack
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package cmd

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
)

func TestGetHistogramMetrics_BucketCount(t *testing.T) {
	histBuckets := []float64{0.05, 0.1, 0.25, 0.5, 0.75}
	labels := []string{"GetObject", "PutObject", "CopyObject", "CompleteMultipartUpload"}
	ttfbHist := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "s3_ttfb_seconds",
			Help:    "Time taken by requests served by current MinIO server instance",
			Buckets: histBuckets,
		},
		[]string{"api"},
	)
	observations := []struct {
		val   float64
		label string
	}{
		{
			val:   0.02,
			label: labels[0],
		},
		{
			val:   0.07,
			label: labels[1],
		},
		{
			val:   0.11,
			label: labels[1],
		},
		{
			val:   0.19,
			label: labels[1],
		},
		{
			val:   0.31,
			label: labels[1],
		},
		{
			val:   0.61,
			label: labels[3],
		},
		{
			val:   0.79,
			label: labels[2],
		},
	}
	ticker := time.NewTicker(1 * time.Millisecond)
	defer ticker.Stop()
	for _, obs := range observations {
		// Send observations once every 1ms, to simulate delay between
		// observations. This is to test the channel based
		// synchronization used internally.
		<-ticker.C
		ttfbHist.With(prometheus.Labels{"api": obs.label}).Observe(obs.val)
	}

	metrics := getHistogramMetrics(ttfbHist, getBucketTTFBDistributionMD(), false, false)
	// additional labels for +Inf for all histogram metrics
	if expPoints := len(labels) * (len(histBuckets) + 1); expPoints != len(metrics) {
		t.Fatalf("Expected %v data points when toLowerAPILabels=false but got %v", expPoints, len(metrics))
	}

	metrics = getHistogramMetrics(ttfbHist, getBucketTTFBDistributionMD(), true, false)
	// additional labels for +Inf for all histogram metrics
	if expPoints := len(labels) * (len(histBuckets) + 1); expPoints != len(metrics) {
		t.Fatalf("Expected %v data points when toLowerAPILabels=true but got %v", expPoints, len(metrics))
	}
}

func TestGetHistogramMetrics_Values(t *testing.T) {
	histBuckets := []float64{0.50, 5.00}
	labels := []string{"PutObject", "CopyObject"}
	ttfbHist := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "s3_ttfb_seconds",
			Help:    "Time taken by requests served by current MinIO server instance",
			Buckets: histBuckets,
		},
		[]string{"api"},
	)
	observations := []struct {
		val   float64
		label string
	}{
		{
			val:   0.02,
			label: labels[0],
		},
		{
			val:   0.19,
			label: labels[1],
		},
		{
			val:   0.31,
			label: labels[1],
		},
		{
			val:   0.61,
			label: labels[0],
		},
		{
			val:   6.79,
			label: labels[1],
		},
	}
	ticker := time.NewTicker(1 * time.Millisecond)
	defer ticker.Stop()
	for _, obs := range observations {
		// Send observations once every 1ms, to simulate delay between
		// observations. This is to test the channel based
		// synchronization used internally.
		<-ticker.C
		ttfbHist.With(prometheus.Labels{"api": obs.label}).Observe(obs.val)
	}

	// Accumulate regular-cased API label metrics for 'PutObject' for deeper verification
	metrics := getHistogramMetrics(ttfbHist, getBucketTTFBDistributionMD(), false, false)
	capitalPutObjects := make([]MetricV2, 0, len(histBuckets)+1)
	for _, metric := range metrics {
		if value := metric.VariableLabels["api"]; value == "PutObject" {
			capitalPutObjects = append(capitalPutObjects, metric)
		}
	}
	if expMetricsPerAPI := len(histBuckets) + 1; expMetricsPerAPI != len(capitalPutObjects) {
		t.Fatalf("Expected %d api=PutObject metrics but got %d", expMetricsPerAPI, len(capitalPutObjects))
	}

	// Deterministic ordering
	slices.SortFunc(capitalPutObjects, func(a MetricV2, b MetricV2) int {
		le1 := a.VariableLabels["le"]
		le2 := a.VariableLabels["le"]
		return strings.Compare(le1, le2)
	})
	if le := capitalPutObjects[0].VariableLabels["le"]; le != "0.500" {
		t.Errorf("Expected le='0.050' api=PutObject metrics but got '%v'", le)
	}
	if value := capitalPutObjects[0].Value; value != 1 {
		t.Errorf("Expected le='0.050' api=PutObject value to be 1 but got '%v'", value)
	}
	if le := capitalPutObjects[1].VariableLabels["le"]; le != "5.000" {
		t.Errorf("Expected le='5.000' api=PutObject metrics but got '%v'", le)
	}
	if value := capitalPutObjects[1].Value; value != 2 {
		t.Errorf("Expected le='5.000' api=PutObject value to be 2 but got '%v'", value)
	}
	if le := capitalPutObjects[2].VariableLabels["le"]; le != "+Inf" {
		t.Errorf("Expected le='+Inf' api=PutObject metrics but got '%v'", le)
	}
	if value := capitalPutObjects[2].Value; value != 2 {
		t.Errorf("Expected le='+Inf' api=PutObject value to be 2 but got '%v'", value)
	}

	// Accumulate lower-cased API label metrics for 'copyobject' for deeper verification
	metrics = getHistogramMetrics(ttfbHist, getBucketTTFBDistributionMD(), true, false)
	lowerCopyObjects := make([]MetricV2, 0, len(histBuckets)+1)
	for _, metric := range metrics {
		if value := metric.VariableLabels["api"]; value == "copyobject" {
			lowerCopyObjects = append(lowerCopyObjects, metric)
		}
	}
	if expMetricsPerAPI := len(histBuckets) + 1; expMetricsPerAPI != len(lowerCopyObjects) {
		t.Fatalf("Expected %d api=copyobject metrics but got %d", expMetricsPerAPI, len(lowerCopyObjects))
	}

	// Deterministic ordering
	slices.SortFunc(lowerCopyObjects, func(a MetricV2, b MetricV2) int {
		le1 := a.VariableLabels["le"]
		le2 := a.VariableLabels["le"]
		return strings.Compare(le1, le2)
	})
	if le := lowerCopyObjects[0].VariableLabels["le"]; le != "0.500" {
		t.Errorf("Expected le='0.050' api=copyobject metrics but got '%v'", le)
	}
	if value := lowerCopyObjects[0].Value; value != 2 {
		t.Errorf("Expected le='0.050' api=copyobject value to be 2 but got '%v'", value)
	}
	if le := lowerCopyObjects[1].VariableLabels["le"]; le != "5.000" {
		t.Errorf("Expected le='5.000' api=copyobject metrics but got '%v'", le)
	}
	if value := lowerCopyObjects[1].Value; value != 2 {
		t.Errorf("Expected le='5.000' api=copyobject value to be 2 but got '%v'", value)
	}
	if le := lowerCopyObjects[2].VariableLabels["le"]; le != "+Inf" {
		t.Errorf("Expected le='+Inf' api=copyobject metrics but got '%v'", le)
	}
	if value := lowerCopyObjects[2].Value; value != 3 {
		t.Errorf("Expected le='+Inf' api=copyobject value to be 3 but got '%v'", value)
	}
}

// unsupportedCollector mimics prometheus' process collector on a platform
// without procfs: its descriptor is invalid, so Register fails.
type unsupportedCollector struct{}

func (unsupportedCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- prometheus.NewInvalidDesc(errors.New("process metrics not supported on this platform"))
}

func (unsupportedCollector) Collect(chan<- prometheus.Metric) {}

// Regression test for https://github.com/minio/minio/issues/21188
func TestRegisterOptionalCollector(t *testing.T) {
	registry := prometheus.NewRegistry()

	// Must not panic.
	registerOptionalCollector(registry, unsupportedCollector{})

	counter := prometheus.NewCounter(prometheus.CounterOpts{Name: "test_counter", Help: "test"})
	registerOptionalCollector(registry, counter)
	counter.Inc()

	mfs, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if len(mfs) != 1 || mfs[0].GetName() != "test_counter" {
		t.Fatalf("unexpected metric families: %v", mfs)
	}
}

// metricsV2Collector exports MetricV2 values the way the cluster and
// bucket collectors do.
type metricsV2Collector []MetricV2

func (metricsV2Collector) Describe(chan<- *prometheus.Desc) {}

func (c metricsV2Collector) Collect(ch chan<- prometheus.Metric) {
	for _, metric := range c {
		labels, values := getOrderedLabelValueArrays(metric.VariableLabels)
		collectMetric(metric, labels, values, "test", ch)
	}
}

// Regression test for https://github.com/minio/minio/issues/17060:
// the TTFB distribution must be exported as a Prometheus histogram.
func TestTTFBDistributionIsHistogram(t *testing.T) {
	newHist := func() *prometheus.HistogramVec {
		return prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "s3_ttfb_seconds",
			Help:    "Time taken by requests served by current MinIO server instance",
			Buckets: []float64{.05, .5, 5},
		}, []string{"api"})
	}
	hist := newHist()
	for _, v := range []float64{0.02, 0.61} {
		hist.WithLabelValues("PutObject").Observe(v)
	}
	for _, v := range []float64{0.19, 0.31, 6.79} {
		hist.WithLabelValues("CopyObject").Observe(v)
	}

	desc := getS3TTFBDistributionMD()
	if desc.Type != histogramMetric {
		t.Fatalf("expected metric type %v, got %v", histogramMetric, desc.Type)
	}
	metrics := getPromHistogramMetrics(hist, desc, true, false)
	if len(metrics) != 2 {
		t.Fatalf("expected one metric per api, got %d", len(metrics))
	}

	const expected = `
# HELP minio_s3_requests_ttfb_seconds_distribution Distribution of time to first byte across API calls
# TYPE minio_s3_requests_ttfb_seconds_distribution histogram
minio_s3_requests_ttfb_seconds_distribution_bucket{api="copyobject",le="0.05"} 0
minio_s3_requests_ttfb_seconds_distribution_bucket{api="copyobject",le="0.5"} 2
minio_s3_requests_ttfb_seconds_distribution_bucket{api="copyobject",le="5"} 2
minio_s3_requests_ttfb_seconds_distribution_bucket{api="copyobject",le="+Inf"} 3
minio_s3_requests_ttfb_seconds_distribution_sum{api="copyobject"} 7.29
minio_s3_requests_ttfb_seconds_distribution_count{api="copyobject"} 3
minio_s3_requests_ttfb_seconds_distribution_bucket{api="putobject",le="0.05"} 1
minio_s3_requests_ttfb_seconds_distribution_bucket{api="putobject",le="0.5"} 1
minio_s3_requests_ttfb_seconds_distribution_bucket{api="putobject",le="5"} 2
minio_s3_requests_ttfb_seconds_distribution_bucket{api="putobject",le="+Inf"} 2
minio_s3_requests_ttfb_seconds_distribution_sum{api="putobject"} 0.63
minio_s3_requests_ttfb_seconds_distribution_count{api="putobject"} 2
`
	if err := testutil.CollectAndCompare(metricsV2Collector(metrics), strings.NewReader(expected)); err != nil {
		t.Fatal(err)
	}

	// The per node collector must export the same histogram, with the server label.
	group := &MetricsGroupV2{}
	group.RegisterRead(func(context.Context) []MetricV2 {
		return getPromHistogramMetrics(hist, desc, true, false)
	})
	reg := prometheus.NewRegistry()
	reg.MustRegister(newMinioCollectorNode([]*MetricsGroupV2{group}))
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if len(mfs) != 1 || mfs[0].GetType() != dto.MetricType_HISTOGRAM || len(mfs[0].GetMetric()) != 2 {
		t.Fatalf("unexpected node metric families: %v", mfs)
	}
	var count uint64
	for _, m := range mfs[0].GetMetric() {
		var hasServer bool
		for _, lp := range m.GetLabel() {
			hasServer = hasServer || lp.GetName() == serverName
		}
		if !hasServer {
			t.Fatalf("missing %s label: %v", serverName, m)
		}
		count += m.GetHistogram().GetSampleCount()
	}
	if count != 5 {
		t.Fatalf("expected 5 samples, got %d", count)
	}
}
