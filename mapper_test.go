package reportportal

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"go.k6.io/k6/v2/metrics"

	"github.com/reportportal/plugin-k6/internal/perf"
)

func samplesFrom(container metrics.SampleContainer) []perf.Sample {
	var out []perf.Sample
	for _, request := range requestsFrom(container) {
		out = append(out, request.sample)
	}
	return out
}

func TestMapsHTTPRequest(t *testing.T) {
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

	got := samplesFrom(container)
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

func TestRequestBodyTagReachesFailureLog(t *testing.T) {
	registry := metrics.NewRegistry()
	duration := registry.MustNewMetric("http_req_duration", metrics.Trend)
	failed := registry.MustNewMetric("http_req_failed", metrics.Trend)
	tags := registry.RootTagSet().
		With("name", "Create Nested Log").
		With("status", "500").
		With("request_body", `{"message":"hi"}`).
		With("expected_response", "false")
	end := time.UnixMilli(1_700_000_000_000)
	container := metrics.ConnectedSamples{
		Time: end,
		Tags: tags,
		Samples: []metrics.Sample{
			{TimeSeries: metrics.TimeSeries{Metric: duration, Tags: tags}, Time: end, Value: 32},
			{TimeSeries: metrics.TimeSeries{Metric: failed, Tags: tags}, Time: end, Value: 1},
		},
	}

	got := samplesFrom(container)
	if len(got) != 1 || got[0].RequestBody == nil || *got[0].RequestBody != `{"message":"hi"}` {
		t.Fatalf("request body %+v", got)
	}
	if detail := perf.FailureDetail(got[0]); !strings.Contains(detail, "--- REQUEST BODY ---\n"+`{"message":"hi"}`) {
		t.Fatalf("detail:\n%s", detail)
	}
}

func TestRequestURLTagReachesFailureLog(t *testing.T) {
	registry := metrics.NewRegistry()
	duration := registry.MustNewMetric("http_req_duration", metrics.Trend)
	failed := registry.MustNewMetric("http_req_failed", metrics.Trend)
	tags := registry.RootTagSet().
		With("name", "Create Nested Log").
		With("url", "Create Nested Log"). // k6 copies name into url
		With("status", "400").
		With("request_url", "https://rp.example.test/api/v1/project/log?x=1").
		With("expected_response", "false")
	end := time.UnixMilli(1_700_000_000_000)
	container := metrics.ConnectedSamples{
		Time: end,
		Tags: tags,
		Samples: []metrics.Sample{
			{TimeSeries: metrics.TimeSeries{Metric: duration, Tags: tags}, Time: end, Value: 12},
			{TimeSeries: metrics.TimeSeries{Metric: failed, Tags: tags}, Time: end, Value: 1},
		},
	}

	got := samplesFrom(container)
	if len(got) != 1 || got[0].URL != "https://rp.example.test/api/v1/project/log?x=1" {
		t.Fatalf("url %+v", got)
	}
	want := "URL/Sampler: Create Nested Log\nURL: https://rp.example.test/api/v1/project/log?x=1\nThread:"
	if detail := perf.FailureDetail(got[0]); !strings.Contains(detail, want) {
		t.Fatalf("detail:\n%s", detail)
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

	got := samplesFrom(container)
	if len(got) != 1 {
		t.Fatalf("samples %d", len(got))
	}
	sample := got[0]
	if sample.Success || sample.ResponseCode != "504" || sample.ResponseMessage != "Gateway Timeout (error_code 1504)" {
		t.Fatalf("result %+v", sample)
	}
	if sample.ResponseBody != nil {
		t.Fatalf("body %v", sample.ResponseBody)
	}
	detail := perf.FailureDetail(sample)
	if strings.Contains(detail, "URL: ") || !strings.Contains(detail, "Response Message: Gateway Timeout (error_code 1504)") {
		t.Fatalf("detail:\n%s", detail)
	}
	if !sample.EndTime().Equal(end) {
		t.Fatalf("end %s", sample.EndTime())
	}
}

func TestGRPCStatusDecidesFailure(t *testing.T) {
	registry := metrics.NewRegistry()
	duration := registry.MustNewMetric("grpc_req_duration", metrics.Trend)
	at := time.UnixMilli(1_700_000_000_000)
	call := func(status string) perf.Sample {
		tags := registry.RootTagSet().With("name", "Say hello").With("status", status)
		got := samplesFrom(metrics.ConnectedSamples{
			Time: at,
			Tags: tags,
			Samples: []metrics.Sample{
				{TimeSeries: metrics.TimeSeries{Metric: duration, Tags: tags}, Time: at, Value: 5},
			},
		})
		if len(got) != 1 {
			t.Fatalf("samples %d", len(got))
		}
		return got[0]
	}

	if ok := call("0"); !ok.Success {
		t.Fatalf("status 0 is OK: %+v", ok)
	}
	if bad := call("14"); bad.Success || bad.ResponseCode != "14" {
		t.Fatalf("status 14 is a failure: %+v", bad)
	}
}

func TestScriptNameBecomesDefaultLaunch(t *testing.T) {
	name := scriptName(&url.URL{Path: "/work/checkout.js"})
	if name != "checkout" {
		t.Fatalf("script name %q", name)
	}
}
