package perf

import "github.com/HdrHistogram/hdrhistogram-go"

const (
	// highestTrackableMs is one hour. Two significant digits match the Java HdrHistogram (~1% error).
	highestTrackableMs int64 = 3_600_000
	significantDigits        = 2
)

// Stats is one aggregated series. Response times live in an HdrHistogram, not in a slice of samples.
type Stats struct {
	Name    string
	Total   int64
	Success int64
	Failed  int64
	Min     int64
	Max     int64
	Sum     int64
	hist    *hdrhistogram.Histogram
	minSet  bool
}

func newStats(name string) *Stats {
	return &Stats{
		Name: name,
		hist: hdrhistogram.New(1, highestTrackableMs, significantDigits),
	}
}

func (s *Stats) add(durationMs int64, success bool) {
	s.Total++
	if success {
		s.Success++
	} else {
		s.Failed++
	}

	duration := durationMs
	if duration < 0 {
		duration = 0
	}
	if duration > highestTrackableMs {
		duration = highestTrackableMs
	}
	s.Sum += duration
	if !s.minSet || duration < s.Min {
		s.Min = duration
		s.minSet = true
	}
	if duration > s.Max {
		s.Max = duration
	}

	// HdrHistogram's lowest trackable value is 1ms, same as Java ConcurrentHistogram.
	// A 0ms sample stays 0 in min/avg and is recorded as 1ms in the histogram.
	histValue := duration
	if histValue < 1 {
		histValue = 1
	}
	_ = s.hist.RecordValue(histValue)
}

// ErrorRate is failed/total as a percent. Empty series return 0.
func (s *Stats) ErrorRate() float64 {
	if s.Total == 0 {
		return 0
	}
	return float64(s.Failed) / float64(s.Total) * 100
}

// Avg is the integer mean, matching Java long division.
func (s *Stats) Avg() int64 {
	if s.Total == 0 {
		return 0
	}
	return s.Sum / s.Total
}

// Percentile returns the histogram value at p (0-100). An empty series returns 0.
func (s *Stats) Percentile(p float64) int64 {
	if s.hist == nil || s.hist.TotalCount() == 0 {
		return 0
	}
	return s.hist.ValueAtPercentile(p)
}

func (s *Stats) snapshot() Stats { return *s }

// Collector aggregates per-label and global latency. The reporter bounds the number of labels.
type Collector struct {
	byName map[string]*Stats
	order  []string
	global *Stats
}

func NewCollector() *Collector {
	return &Collector{
		byName: map[string]*Stats{},
		global: newStats("Global"),
	}
}

// Record adds one sample under name.
func (c *Collector) Record(name string, durationMs int64, success bool) {
	stats, ok := c.byName[name]
	if !ok {
		stats = newStats(name)
		c.byName[name] = stats
		c.order = append(c.order, name)
	}
	stats.add(durationMs, success)
	c.global.add(durationMs, success)
}

// Series returns per-label stats in first-seen order.
func (c *Collector) Series() []Stats {
	out := make([]Stats, 0, len(c.order))
	for _, name := range c.order {
		out = append(out, c.byName[name].snapshot())
	}
	return out
}

// ByName returns the stats for one label.
func (c *Collector) ByName(name string) (Stats, bool) {
	stats, ok := c.byName[name]
	if !ok {
		return Stats{}, false
	}
	return stats.snapshot(), true
}

// Global returns the merged series.
func (c *Collector) Global() Stats {
	return c.global.snapshot()
}
