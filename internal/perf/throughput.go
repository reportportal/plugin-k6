package perf

import (
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
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	return sign + itoa(cents/100) + "." + twoDigits(cents%100)
}

func twoDigits(v int64) string {
	if v < 10 {
		return "0" + itoa(v)
	}
	return itoa(v)
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

// Aggregator collects spans during the run. Snapshot is called once at the end.
type Aggregator struct {
	spans []Span
}

func (a *Aggregator) Record(startMs, endMs int64) {
	if endMs < startMs {
		endMs = startMs
	}
	a.spans = append(a.spans, Span{StartMs: startMs, EndMs: endMs})
}

// Snapshot computes overall mean and peak throughput.
// WindowSeconds below 1 is treated as the default of 1.
// The lifecycle is [earliest start, latest completion]. A run shorter than the
// window uses the overall mean as the peak, so a sub-second burst is not divided
// by the full window. A zero-length timeline yields 0.00.
func (a *Aggregator) Snapshot(windowSeconds int) Throughput {
	return Calculate(a.spans, windowSeconds)
}

// Calculate is the pure throughput math shared with the Java plugin.
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
		start, end := span.StartMs, span.EndMs
		if end < start {
			end = start
		}
		if start < tStart {
			tStart = start
		}
		if end > tEnd {
			tEnd = end
		}
		ends[i] = end
	}

	duration := tEnd - tStart
	if duration < 0 {
		duration = 0
	}
	overall := rpsCents(int64(len(spans)), duration)
	peak := overall
	windowMs := int64(windowSeconds) * 1000
	if duration > 0 && duration >= windowMs {
		peak = peakCents(ends, windowMs)
	}
	if duration <= 0 {
		overall = 0
		peak = 0
	}
	return Throughput{
		OverallCents: overall,
		PeakCents:    peak,
		Total:        int64(len(spans)),
		DurationMs:   duration,
	}
}

func peakCents(endTimes []int64, windowMs int64) int64 {
	if len(endTimes) == 0 || windowMs <= 0 {
		return 0
	}
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
