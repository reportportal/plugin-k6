package perf

import (
	"fmt"
	"strings"
)

const responseBodyLimit = 3000

// GlobalMetricsMarkdown renders the launch-wide latency table.
func GlobalMetricsMarkdown(stats Stats) string {
	return "## Global metrics\n\n" + fmt.Sprintf(
		"| Total | Success | Failed | Error %% | Min | Max | Avg | p50 | p95 | p99 |\n"+
			"| :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: |\n"+
			"| %d | %d | %d | %.2f%% | %d | %d | %d | %d | %d | %d |\n",
		stats.Total,
		stats.Success,
		stats.Failed,
		stats.ErrorRate(),
		stats.Min,
		stats.Max,
		stats.Avg(),
		stats.Percentile(50),
		stats.Percentile(95),
		stats.Percentile(99),
	)
}

// ThroughputMarkdown renders overall mean and peak throughput.
func ThroughputMarkdown(throughput Throughput) string {
	return "## Throughput\n\n" +
		"| Overall mean (req/s) | Peak (req/s) |\n" +
		"| :---: | :---: |\n" +
		fmt.Sprintf("| %s | %s |\n", throughput.FormatOverall(), throughput.FormatPeak())
}

// PerRequestMarkdown renders one row per request label, in the given order.
func PerRequestMarkdown(series []Stats) string {
	var b strings.Builder
	b.WriteString("## Per-request metrics\n\n")
	b.WriteString("| Request | Total | Success | Failed | Error % | Min (ms) | Max (ms) | Avg (ms) | 50% (ms) | 95% (ms) | 99% (ms) |\n")
	b.WriteString("| :--- | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: |\n")
	for _, stats := range series {
		fmt.Fprintf(&b,
			"| **%s** | %d | %d | %d | %.2f%% | %d | %d | %d | %d | %d | %d |\n",
			stats.Name,
			stats.Total,
			stats.Success,
			stats.Failed,
			stats.ErrorRate(),
			stats.Min,
			stats.Max,
			stats.Avg(),
			stats.Percentile(50),
			stats.Percentile(95),
			stats.Percentile(99),
		)
	}
	return b.String()
}

// RequestAttributes are the attributes finished onto each request SUITE.
func RequestAttributes(stats Stats) []Attribute {
	if stats.Total <= 0 {
		return nil
	}
	return []Attribute{
		{Key: "total", Value: fmt.Sprintf("%d", stats.Total)},
		{Key: "failed", Value: fmt.Sprintf("%d", stats.Failed)},
		{Key: "error_rate_pct", Value: fmt.Sprintf("%.2f", stats.ErrorRate())},
		{Key: "avg_ms", Value: fmt.Sprintf("%d", stats.Avg())},
		{Key: "p50_ms", Value: fmt.Sprintf("%d", stats.Percentile(50))},
		{Key: "p95_ms", Value: fmt.Sprintf("%d", stats.Percentile(95))},
		{Key: "p99_ms", Value: fmt.Sprintf("%d", stats.Percentile(99))},
	}
}

// LaunchAttributes are the latency, throughput, and optional SLA attributes on the launch.
func LaunchAttributes(stats Stats, throughput Throughput, sla SLAConfig, result SLAResult, custom []Attribute) []Attribute {
	if stats.Total <= 0 {
		return nil
	}
	attrs := []Attribute{
		{Key: "p50_ms", Value: fmt.Sprintf("%d", stats.Percentile(50))},
		{Key: "p95_ms", Value: fmt.Sprintf("%d", stats.Percentile(95))},
		{Key: "p99_ms", Value: fmt.Sprintf("%d", stats.Percentile(99))},
		{Key: "throughput_rps", Value: throughput.FormatOverall()},
		{Key: "peak_throughput_rps", Value: throughput.FormatPeak()},
	}
	if sla.HasAnyThreshold() {
		value := "PASS"
		if !result.Passed {
			value = "FAIL"
		}
		attrs = append(attrs, Attribute{Key: "sla", Value: value})
	}
	return append(attrs, custom...)
}

// FailureDetail is the ERROR log body for one failed sample.
func FailureDetail(sample Sample) string {
	requestBody := "No Request Data"
	if sample.RequestBody != nil {
		requestBody = *sample.RequestBody
	}
	responseBody := "No Response Data"
	if sample.ResponseBody != nil {
		responseBody = Truncate(*sample.ResponseBody, responseBodyLimit)
	}
	urlLine := ""
	if sample.URL != "" {
		urlLine = "URL: " + sample.URL + "\n"
	}
	return fmt.Sprintf(
		"REQUEST FAILURE DETAIL:\n\n"+
			"URL/Sampler: %s\n"+
			"%s"+
			"Thread: %s\n"+
			"Elapsed: %d ms\n"+
			"Response Code: %s\n"+
			"Response Message: %s\n\n"+
			"--- REQUEST BODY ---\n%s\n\n"+
			"--- RESPONSE BODY ---\n%s",
		sample.Label,
		urlLine,
		sample.Thread,
		sample.DurationMs,
		sample.ResponseCode,
		sample.ResponseMessage,
		requestBody,
		responseBody,
	)
}

// SampleStepName is the failed-sample STEP name: "<thread> | <code> | <ms> ms".
func SampleStepName(sample Sample) string {
	thread := sample.Thread
	if thread == "" {
		thread = "thread"
	}
	code := sample.ResponseCode
	if code == "" {
		code = "-"
	}
	return fmt.Sprintf("%s | %s | %d ms", thread, code, sample.DurationMs)
}

// Truncate cuts text to maxLength runes and marks the cut, matching the Java formatter.
func Truncate(text string, maxLength int) string {
	runes := []rune(text)
	if text == "" || len(runes) <= maxLength {
		return text
	}
	return string(runes[:maxLength]) + "\n... [TRUNCATED] ..."
}
