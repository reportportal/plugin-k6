package perf

import "time"

// Sample is one completed request, independent of the load tool that produced it.
// Timestamp is the request start. The completion time is Timestamp plus DurationMs.
type Sample struct {
	Label           string
	URL             string
	Scenario        string
	Thread          string
	Timestamp       time.Time
	DurationMs      int64
	Success         bool
	ResponseCode    string
	ResponseMessage string
	RequestBody     *string
	ResponseBody    *string
}

// EndTime is the request completion time.
func (s Sample) EndTime() time.Time {
	if s.DurationMs <= 0 {
		return s.Timestamp
	}
	return s.Timestamp.Add(time.Duration(s.DurationMs) * time.Millisecond)
}
