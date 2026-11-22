package perf

import (
	"fmt"
	"math"
	"sort"
)

// Span is one request's start and completion, in epoch milliseconds.
// Throughput counts every completion, success and failure alike.
type Span struct {
	StartMs int64
	EndMs   int64
}

// Throughput is requests per second already rounded half-up to 2 decimal places.
// Values are stored as hundredths of a request so formatting does not drift.
type Throughput struct {
	OverallCents int64
	PeakCents    int64
	Total        int64
	DurationMs   int64
}

func (t Throughput) OverallRps() float64 { return float64(t.OverallCents) / 100 }

func (t Throughput) PeakRps() float64 { return float64(t.PeakCents) / 100 }

func (t Throughput) FormatOverall() string { return formatCents(t.OverallCents) }

func (t Throughput) FormatPeak() string { return formatCents(t.PeakCents) }

func formatCents(cents int64) string {
	return fmt.Sprintf("%d.%02d", cents/100, cents%100)
}

// Aggregator collects spans during the run. Snapshot is called once at the end.
type Aggregator struct {
	spans []Span
}

func (a *Aggregator) Record(startMs, endMs int64) {
	a.spans = append(a.spans, Span{StartMs: startMs, EndMs: endMs})
}

// Snapshot computes overall mean and peak throughput over the recorded spans.
func (a *Aggregator) Snapshot(windowSeconds int) Throughput {
	return Calculate(a.spans, windowSeconds)
}

// Calculate is the pure throughput math shared with the Java plugin.
// The lifecycle is [earliest start, latest completion]. A run shorter than the
// window uses the overall mean as the peak, so a sub-second burst is not divided
// by the full window. A zero-length timeline yields 0.00. WindowSeconds below 1 is 1.
func Calculate(spans []Span, windowSeconds int) Throughput {
	if windowSeconds < 1 {
		windowSeconds = 1
	}
	if len(spans) == 0 {
		return Throughput{}
	}

	tStart := int64(math.MaxInt64)
	tEnd := int64(math.MinInt64)
	ends := make([]int64, len(spans))
	for i, span := range spans {
		end := max(span.EndMs, span.StartMs)
		tStart = min(tStart, span.StartMs)
		tEnd = max(tEnd, end)
		ends[i] = end
	}

	duration := tEnd - tStart
	overall := rpsCents(int64(len(spans)), duration)
	peak := overall
	windowMs := int64(windowSeconds) * 1000
	if duration >= windowMs {
		peak = peakCents(ends, windowMs)
	}
	return Throughput{
		OverallCents: overall,
		PeakCents:    peak,
		Total:        int64(len(spans)),
		DurationMs:   duration,
	}
}

func peakCents(endTimes []int64, windowMs int64) int64 {
	sorted := append([]int64(nil), endTimes...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	maxCount := 0
	left := 0
	for right := 0; right < len(sorted); right++ {
		for sorted[right]-sorted[left] >= windowMs {
			left++
		}
		if count := right - left + 1; count > maxCount {
			maxCount = count
		}
	}
	return rpsCents(int64(maxCount), windowMs)
}

// rpsCents is requests / (durationMs/1000), rounded half-up to 2 decimal places,
// returned as hundredths. Zero requests or zero duration is 0.
func rpsCents(requests, durationMs int64) int64 {
	if requests <= 0 || durationMs <= 0 {
		return 0
	}
	num := requests * 1000 * 100
	quot := num / durationMs
	rem := num % durationMs
	if rem*2 >= durationMs {
		quot++
	}
	return quot
}
