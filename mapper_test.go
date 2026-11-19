package reportportal

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"go.k6.io/k6/v2/metrics"

	"github.com/reportportal/plugin-performance-k6/internal/perf"
)

func TestSamplesFromHTTPRequest(t *testing.T) {
	registry := metrics.NewRegistry()
	duration := registry.MustNewMetric("http_req_duration", metrics.Trend)
	failed := registry.MustNewMetric("http_req_failed", metrics.Trend)
	tags := registry.RootTagSet().
		With("name", "GET /items").
		With("scenario", "checkout").
		With("status", "500").
		With("expected_response", "false").
		With("error", "boom").
		With("group", "::Checkout::Pay").
		With("vu", "3")
	end := time.UnixMilli(1_700_000_000_000)
	container := metrics.ConnectedSamples{
		Time: end,
		Tags: tags,
		Samples: []metrics.Sample{
			{TimeSeries: metrics.TimeSeries{Metric: duration, Tags: tags}, Time: end, Value: 120.4},
			{TimeSeries: metrics.TimeSeries{Metric: failed, Tags: tags}, Time: end, Value: 1},
		},
	}

	got := SamplesFrom(container)
	if len(got) != 1 {
		t.Fatalf("samples %d", len(got))
	}
	sample := got[0]
	if sample.Label != "Checkout / Pay / GET /items" || sample.Scenario != "checkout" || sample.Thread != "vu-3" {
		t.Fatalf("identity %+v", sample)
	}
	if sample.DurationMs != 120 || sample.Success || sample.ResponseCode != "500" || sample.ResponseMessage != "boom" {
		t.Fatalf("result %+v", sample)
	}
	if sample.ResponseBody == nil || *sample.ResponseBody != "boom" {
		t.Fatalf("body %+v", sample.ResponseBody)
	}
}

func TestHTTPStatusWithoutErrorTag(t *testing.T) {
	registry := metrics.NewRegistry()
	duration := registry.MustNewMetric("http_req_duration", metrics.Trend)
	failed := registry.MustNewMetric("http_req_failed", metrics.Trend)
	tags := registry.RootTagSet().
		With("name", "Create Nested Log").
		With("group", "::reporting").
		With("status", "504").
		With("url", "https://example.test/api/v1/log").
		With("error_code", "1504").
		With("expected_response", "false")
	end := time.UnixMilli(1_700_000_000_000)
	container := metrics.ConnectedSamples{
		Time: end,
		Tags: tags,
		Samples: []metrics.Sample{
			{TimeSeries: metrics.TimeSeries{Metric: duration, Tags: tags}, Time: end, Value: 10001},
			{TimeSeries: metrics.TimeSeries{Metric: failed, Tags: tags}, Time: end, Value: 1},
		},
	}

	got := SamplesFrom(container)
	if len(got) != 1 {
		t.Fatalf("samples %d", len(got))
	}
	sample := got[0]
	if sample.Success || sample.ResponseCode != "504" || sample.ResponseMessage != "Gateway Timeout (error_code 1504)" {
		t.Fatalf("result %+v", sample)
	}
	if sample.URL != "https://example.test/api/v1/log" || sample.ResponseBody != nil {
		t.Fatalf("url %q body %v", sample.URL, sample.ResponseBody)
	}
	detail := perf.FailureDetail(sample)
	if !strings.Contains(detail, "URL: https://example.test/api/v1/log") || !strings.Contains(detail, "Response Message: Gateway Timeout (error_code 1504)") {
		t.Fatalf("detail:\n%s", detail)
	}
	if !sample.EndTime().Equal(end) {
		t.Fatalf("end %s", sample.EndTime())
	}
}

func TestScriptNameBecomesDefaultLaunch(t *testing.T) {
	name := scriptName(&url.URL{Path: "/work/checkout.js"})
	if name != "checkout" {
		t.Fatalf("script name %q", name)
	}
}
