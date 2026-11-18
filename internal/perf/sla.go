package perf

import (
	"fmt"
	"strings"
)

// Status is the result of one SLA check.
type Status string

const (
	StatusPass    Status = "PASS"
	StatusFail    Status = "FAIL"
	StatusSkipped Status = "SKIPPED"
)

// SLAConfig holds optional global thresholds. A nil field disables that check.
type SLAConfig struct {
	P95Ms                *int64
	P99Ms                *int64
	ErrorRatePct         *float64
	MinThroughputRps     *float64
	MinPeakThroughputRps *float64
}

// HasAnyThreshold reports whether at least one check is configured.
func (c SLAConfig) HasAnyThreshold() bool {
	return c.P95Ms != nil || c.P99Ms != nil || c.ErrorRatePct != nil ||
		c.MinThroughputRps != nil || c.MinPeakThroughputRps != nil
}

// Check is one row of the quality gate.
type Check struct {
	Metric string
	Actual string
	Target string
	Status Status
}

// SLAResult is the quality gate for the whole launch.
type SLAResult struct {
	Checks []Check
	Passed bool
}

// Evaluate compares global stats and throughput with the configured thresholds.
func Evaluate(config SLAConfig, stats Stats, throughput Throughput) SLAResult {
	var checks []Check

	if config.P95Ms != nil {
		actual := stats.Percentile(95)
		target := *config.P95Ms
		checks = append(checks, Check{
			Metric: "p95",
			Actual: fmt.Sprintf("%d ms", actual),
			Target: fmt.Sprintf("<= %d ms", target),
			Status: passIf(actual <= target),
		})
	}
	if config.P99Ms != nil {
		actual := stats.Percentile(99)
		target := *config.P99Ms
		checks = append(checks, Check{
			Metric: "p99",
			Actual: fmt.Sprintf("%d ms", actual),
			Target: fmt.Sprintf("<= %d ms", target),
			Status: passIf(actual <= target),
		})
	}
	if config.ErrorRatePct != nil {
		actual := stats.ErrorRate()
		target := *config.ErrorRatePct
		checks = append(checks, Check{
			Metric: "error_rate",
			Actual: fmt.Sprintf("%.2f%%", actual),
			Target: fmt.Sprintf("<= %.2f%%", target),
			Status: passIf(actual <= target),
		})
	}
	if config.MinThroughputRps != nil {
		target := *config.MinThroughputRps
		checks = append(checks, Check{
			Metric: "throughput_rps",
			Actual: throughput.FormatOverall() + " req/s",
			Target: fmt.Sprintf(">= %.2f req/s", target),
			Status: passIf(throughput.OverallRps() >= target),
		})
	}
	if config.MinPeakThroughputRps != nil {
		target := *config.MinPeakThroughputRps
		checks = append(checks, Check{
			Metric: "peak_throughput_rps",
			Actual: throughput.FormatPeak() + " req/s",
			Target: fmt.Sprintf(">= %.2f req/s", target),
			Status: passIf(throughput.PeakRps() >= target),
		})
	}

	passed := true
	for _, check := range checks {
		if check.Status == StatusFail {
			passed = false
			break
		}
	}
	return SLAResult{Checks: checks, Passed: passed}
}

func passIf(ok bool) Status {
	if ok {
		return StatusPass
	}
	return StatusFail
}

// SLAMarkdown is the summary-log block.
func SLAMarkdown(result SLAResult) string {
	if len(result.Checks) == 0 {
		return "## SLA / Quality Gate\n\nNo SLA thresholds configured.\n"
	}
	var b strings.Builder
	b.WriteString("## SLA / Quality Gate\n\n**Overall: ")
	if result.Passed {
		b.WriteString("PASS")
	} else {
		b.WriteString("FAIL")
	}
	b.WriteString("**\n\n")
	writeSLATable(&b, result, true)
	return b.String()
}

// SLALaunchDescription is the compact block stored on the launch.
func SLALaunchDescription(result SLAResult) string {
	if len(result.Checks) == 0 {
		return "No SLA thresholds configured."
	}
	var b strings.Builder
	b.WriteString("Overall: ")
	if result.Passed {
		b.WriteString("PASS")
	} else {
		b.WriteString("FAIL")
	}
	b.WriteString("\n\n")
	writeSLATable(&b, result, false)
	return b.String()
}

func writeSLATable(b *strings.Builder, result SLAResult, boldStatus bool) {
	b.WriteString("| Metric | Actual | Target | Result |\n")
	b.WriteString("| :--- | :---: | :---: | :---: |\n")
	for _, check := range result.Checks {
		status := string(check.Status)
		if boldStatus {
			status = "**" + status + "**"
		}
		fmt.Fprintf(b, "| %s | %s | %s | %s |\n", check.Metric, check.Actual, check.Target, status)
	}
}
