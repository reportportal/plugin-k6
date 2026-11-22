package reportportal

import (
	"sort"

	"go.k6.io/k6/v2/metrics"

	"github.com/reportportal/plugin-performance-k6/internal/perf"
)

// bodyPairer holds each failed request until its response body arrives.
//
// The script writes the body right after the failed response and before the same
// VU starts its next request. A held request is therefore ready when its body
// arrives, when the VU makes another request (no body was sent), or at Stop.
// Successful requests are never held.
type bodyPairer struct {
	held map[string]heldRequest // keyed by VU
}

type heldRequest struct {
	key    string
	sample perf.Sample
}

func newBodyPairer() *bodyPairer {
	return &bodyPairer{held: map[string]heldRequest{}}
}

// Push takes one k6 sample container and returns the samples that are ready to report, in order.
func (p *bodyPairer) Push(container metrics.SampleContainer) []perf.Sample {
	var ready []perf.Sample
	for _, body := range responseBodiesFrom(container) {
		held, ok := p.held[body.vu]
		if !ok || held.key != body.key {
			continue
		}
		delete(p.held, body.vu)
		text := body.text
		held.sample.ResponseBody = &text
		ready = append(ready, held.sample)
	}
	for _, request := range requestsFrom(container) {
		if prev, ok := p.held[request.vu]; ok {
			delete(p.held, request.vu)
			ready = append(ready, prev.sample)
		}
		if request.sample.Success {
			ready = append(ready, request.sample)
			continue
		}
		p.held[request.vu] = heldRequest{key: request.key, sample: request.sample}
	}
	return ready
}

// Flush returns every request that is still held, oldest first. Call it when the test stops.
func (p *bodyPairer) Flush() []perf.Sample {
	out := make([]perf.Sample, 0, len(p.held))
	for vu, held := range p.held {
		out = append(out, held.sample)
		delete(p.held, vu)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Timestamp.Before(out[j].Timestamp)
	})
	return out
}
