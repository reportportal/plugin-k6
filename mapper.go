package reportportal

import (
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.k6.io/k6/v2/metrics"

	"github.com/reportportal/plugin-performance-k6/internal/perf"
)

// SamplesFrom converts one k6 sample batch into request samples.
// Only http_req_duration and grpc_req_duration are requests. k6 emits those
// once per call; submetric series are ignored by matching the metric name exactly.
// The sample timestamp is the request completion time.
func SamplesFrom(container metrics.SampleContainer) []perf.Sample {
	if container == nil {
		return nil
	}
	raw := container.GetSamples()
	var out []perf.Sample
	for _, sample := range raw {
		if sample.Metric == nil {
			continue
		}
		switch sample.Metric.Name {
		case "http_req_duration", "grpc_req_duration":
		default:
			continue
		}
		out = append(out, mapSample(container, raw, sample))
	}
	return out
}

func mapSample(container metrics.SampleContainer, batch []metrics.Sample, sample metrics.Sample) perf.Sample {
	name := tag(container, sample, "name")
	if name == "" {
		method := tag(container, sample, "method")
		url := tag(container, sample, "url")
		switch {
		case method != "" && url != "":
			name = method + " " + url
		case url != "":
			name = url
		default:
			name = "request"
		}
	}
	if prefix := groupPrefix(tag(container, sample, "group")); prefix != "" {
		name = prefix + " / " + name
	}

	scenario := tag(container, sample, "scenario")
	if strings.TrimSpace(scenario) == "" {
		scenario = "Unknown"
	}
	thread := "vu"
	if vu := tag(container, sample, "vu"); vu != "" {
		thread = "vu-" + vu
	}

	duration := int64(math.Round(sample.Value))
	if duration < 0 || math.IsNaN(sample.Value) || math.IsInf(sample.Value, 0) {
		duration = 0
	}
	end := sample.Time
	if end.IsZero() {
		if connected, ok := container.(metrics.ConnectedSampleContainer); ok {
			end = connected.GetTime()
		}
	}
	if end.IsZero() {
		end = time.Now()
	}
	start := end.Add(-time.Duration(duration) * time.Millisecond)

	code := tag(container, sample, "status")
	// k6 sets the error tag only for transport failures. An HTTP 4xx/5xx still
	// completes, and the only extra signal is error_code = 1000 + status.
	errorText := tag(container, sample, "error")
	message := responseMessage(code, errorText, tag(container, sample, "error_code"))
	var responseBody *string
	if errorText != "" {
		responseBody = &errorText
	}
	// The script sends the JSON body as a request_body tag when REPORT_REQUEST_BODIES is on.
	var requestBody *string
	if body := tag(container, sample, "request_body"); body != "" {
		requestBody = &body
	}

	return perf.Sample{
		Label:           name,
		Scenario:        scenario,
		Thread:          thread,
		Timestamp:       start,
		DurationMs:      duration,
		Success:         !failed(batch, sample, container),
		ResponseCode:    code,
		ResponseMessage: message,
		RequestBody:     requestBody,
		ResponseBody:    responseBody,
	}
}

// responseMessage prefers k6's error string. Otherwise it uses the HTTP reason
// phrase, and the k6 error_code when that phrase is missing.
func responseMessage(status, errorText, errorCode string) string {
	if errorText != "" {
		return errorText
	}
	message := ""
	if code, err := strconv.Atoi(status); err == nil && code > 0 {
		message = http.StatusText(code)
	}
	if errorCode == "" {
		return message
	}
	if message == "" {
		return "k6 error_code " + errorCode
	}
	return message + " (error_code " + errorCode + ")"
}

func failed(batch []metrics.Sample, duration metrics.Sample, container metrics.SampleContainer) bool {
	failedName := strings.TrimSuffix(duration.Metric.Name, "duration") + "failed"
	for _, sample := range batch {
		if sample.Metric != nil && sample.Metric.Name == failedName {
			return sample.Value != 0
		}
	}
	if tag(container, duration, "expected_response") == "false" {
		return true
	}
	return tag(container, duration, "error") != ""
}

func tag(container metrics.SampleContainer, sample metrics.Sample, key string) string {
	if sample.Tags != nil {
		if value, ok := sample.Tags.Get(key); ok && value != "" {
			return value
		}
	}
	if value := sample.Metadata[key]; value != "" {
		return value
	}
	if connected, ok := container.(metrics.ConnectedSampleContainer); ok && connected.GetTags() != nil {
		if value, ok := connected.GetTags().Get(key); ok {
			return value
		}
	}
	return ""
}

func groupPrefix(group string) string {
	group = strings.TrimPrefix(group, "::")
	if strings.Trim(group, ":") == "" {
		return ""
	}
	parts := strings.Split(group, "::")
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(part, ":")
		part = strings.TrimSpace(part)
		if part != "" {
			cleaned = append(cleaned, part)
		}
	}
	return strings.Join(cleaned, " / ")
}
