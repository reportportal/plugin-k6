package reportportal

import (
	"testing"
	"time"

	"go.k6.io/k6/v2/metrics"
)

type pairingFixture struct {
	duration *metrics.Metric
	failed   *metrics.Metric
	body     *metrics.Metric
	root     *metrics.TagSet
}

func newPairingFixture() pairingFixture {
	registry := metrics.NewRegistry()
	return pairingFixture{
		duration: registry.MustNewMetric("http_req_duration", metrics.Trend),
		failed:   registry.MustNewMetric("http_req_failed", metrics.Trend),
		body:     registry.MustNewMetric("rp_response_body", metrics.Counter),
		root:     registry.RootTagSet(),
	}
}

// request is one k6 HTTP request: the duration sample plus its failure rate.
func (f pairingFixture) request(vu, name, status string, failed bool, at time.Time) metrics.SampleContainer {
	tags := f.root.With("name", name).With("vu", vu).With("status", status).With("scenario", "default")
	samples := []metrics.Sample{
		{TimeSeries: metrics.TimeSeries{Metric: f.duration, Tags: tags}, Time: at, Value: 12},
	}
	if failed {
		tags = tags.With("expected_response", "false")
		samples[0].Tags = tags
		samples = append(samples, metrics.Sample{TimeSeries: metrics.TimeSeries{Metric: f.failed, Tags: tags}, Time: at, Value: 1})
	} else {
		tags = tags.With("expected_response", "true")
		samples[0].Tags = tags
		samples = append(samples, metrics.Sample{TimeSeries: metrics.TimeSeries{Metric: f.failed, Tags: tags}, Time: at, Value: 0})
	}
	return metrics.ConnectedSamples{Time: at, Tags: tags, Samples: samples}
}

// responseBody is the rp_response_body counter the script writes after a failed response.
func (f pairingFixture) responseBody(vu, name, text string, at time.Time) metrics.SampleContainer {
	tags := f.root.With("name", name).With("vu", vu).With("response_body", text)
	return metrics.ConnectedSamples{
		Time: at,
		Tags: tags,
		Samples: []metrics.Sample{
			{TimeSeries: metrics.TimeSeries{Metric: f.body, Tags: tags}, Time: at, Value: 1},
		},
	}
}

func TestResponseBodyAttachesToFailedRequest(t *testing.T) {
	f := newPairingFixture()
	pairer := newBodyPairer()
	at := time.UnixMilli(1_700_000_000_000)

	if got := pairer.Push(f.request("1", "Error 401", "401", true, at)); len(got) != 0 {
		t.Fatalf("failed request must wait for its body, got %d", len(got))
	}
	got := pairer.Push(f.responseBody("1", "Error 401", `{"message":"Unauthorized"}`, at))
	if len(got) != 1 {
		t.Fatalf("ready samples %d", len(got))
	}
	sample := got[0]
	if sample.Success || sample.ResponseCode != "401" {
		t.Fatalf("sample %+v", sample)
	}
	if sample.ResponseBody == nil || *sample.ResponseBody != `{"message":"Unauthorized"}` {
		t.Fatalf("body %v", sample.ResponseBody)
	}
	if rest := pairer.Flush(); len(rest) != 0 {
		t.Fatalf("nothing should be left held, got %d", len(rest))
	}
}

func TestBodyFromAnotherVUIsNotAttached(t *testing.T) {
	f := newPairingFixture()
	pairer := newBodyPairer()
	at := time.UnixMilli(1_700_000_000_000)

	pairer.Push(f.request("1", "Error 404", "404", true, at))
	if got := pairer.Push(f.responseBody("2", "Error 404", "other vu", at)); len(got) != 0 {
		t.Fatalf("body from VU 2 must not release VU 1, got %d", len(got))
	}
	rest := pairer.Flush()
	if len(rest) != 1 || rest[0].ResponseBody != nil {
		t.Fatalf("held request %+v", rest)
	}
}

func TestHeldRequestIsReleasedByNextRequestFromSameVU(t *testing.T) {
	f := newPairingFixture()
	pairer := newBodyPairer()
	at := time.UnixMilli(1_700_000_000_000)

	pairer.Push(f.request("1", "Error Timeout", "0", true, at))
	got := pairer.Push(f.request("1", "Create Launch", "200", false, at.Add(time.Second)))
	if len(got) != 2 {
		t.Fatalf("ready samples %d", len(got))
	}
	if got[0].Success || got[0].ResponseBody != nil {
		t.Fatalf("held failure should be released without a body: %+v", got[0])
	}
	if !got[1].Success {
		t.Fatalf("second request should be a success: %+v", got[1])
	}
}

func TestFlushReturnsHeldRequestsOldestFirst(t *testing.T) {
	f := newPairingFixture()
	pairer := newBodyPairer()
	first := time.UnixMilli(1_700_000_000_000)

	pairer.Push(f.request("2", "Error 500", "500", true, first.Add(time.Second)))
	pairer.Push(f.request("1", "Error 401", "401", true, first))

	got := pairer.Flush()
	if len(got) != 2 {
		t.Fatalf("flushed %d", len(got))
	}
	if got[0].ResponseCode != "401" || got[1].ResponseCode != "500" {
		t.Fatalf("order %s then %s", got[0].ResponseCode, got[1].ResponseCode)
	}
}
