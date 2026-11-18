package perf

import (
	"fmt"
	"sync"
	"time"
)

const (
	maxFailedSamplesPerRequest = 20
	requestKeySep              = "\x00"
)

// Reporter turns request samples into the ReportPortal launch hierarchy shared
// with the JMeter and Gatling plugins.
type Reporter struct {
	portal        Portal
	logger        Logger
	sla           SLAConfig
	windowSeconds int
	custom        []Attribute
	stats         *Collector
	throughput    *Aggregator
	scenarios     []*scenarioNode
	scenarioIndex map[string]*scenarioNode
	labelOverflow bool
	summaryID     string
	started       bool
	closed        bool
	mu            sync.Mutex
}

type scenarioNode struct {
	name     string
	first    time.Time
	id       string
	requests []*requestNode
	index    map[string]*requestNode
}

type requestNode struct {
	key      string
	label    string
	scenario string
	first    time.Time
	failures int
	reported []Sample
	capped   bool
	id       string
}

// NewReporter builds a reporter. portal and the SLA/attribute values are fixed for the run.
func NewReporter(portal Portal, sla SLAConfig, windowSeconds int, custom []Attribute, logger Logger) *Reporter {
	if windowSeconds < 1 {
		windowSeconds = 1
	}
	return &Reporter{
		portal:        portal,
		logger:        logger,
		sla:           sla,
		windowSeconds: windowSeconds,
		custom:        append([]Attribute(nil), custom...),
		stats:         NewCollector(),
		throughput:    &Aggregator{},
		scenarioIndex: map[string]*scenarioNode{},
	}
}

// Init opens the launch and the summary item so the summary stays first in the tree.
func (r *Reporter) Init(launchName string, start time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started {
		return nil
	}
	launchID, err := r.portal.StartLaunch(launchName, start, r.custom)
	if err != nil {
		return err
	}
	if launchID == "" {
		return fmt.Errorf("ReportPortal did not return a launch id")
	}
	summaryID, err := r.portal.StartRootItem("Performance Summary Report", "STEP", start)
	if err != nil {
		_ = r.portal.FinishLaunch("FAILED", time.Now())
		return err
	}
	r.summaryID = summaryID
	r.started = true
	infof(r.logger, "ReportPortal launch %q started", launchName)
	return nil
}

// Process records one request. Safe to call from the k6 output callback.
func (r *Reporter) Process(sample Sample) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.started || r.closed {
		return
	}
	label := r.trackedLabel(sample)
	r.stats.Record(label, sample.DurationMs, sample.Success)
	r.throughput.Record(sample.Timestamp.UnixMilli(), sample.EndTime().UnixMilli())
	r.remember(sample, label)
}

// Shutdown writes the summary, the scenario tree, and finishes the launch.
// The launch status follows the SLA only: request failures are visible on the
// request items, and without thresholds the launch is PASSED. That matches the
// JMeter and Gatling plugins.
func (r *Reporter) Shutdown() error {
	r.mu.Lock()
	if !r.started || r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	summaryID := r.summaryID
	scenarios := r.scenarios
	stats := r.stats
	throughput := r.throughput.Snapshot(r.windowSeconds)
	sla := r.sla
	custom := r.custom
	r.mu.Unlock()

	infof(r.logger, "Generating aggregated performance report")
	global := stats.Global()
	result := Evaluate(sla, global, throughput)
	now := time.Now()

	var first error
	note := func(err error) {
		if err != nil && first == nil {
			first = err
			errorf(r.logger, "ReportPortal reporting error: %v", err)
		}
	}

	note(r.portal.Log(summaryID, "INFO", "# FINAL PERFORMANCE AGGREGATED REPORT", now))
	level := "INFO"
	slaFailed := sla.HasAnyThreshold() && !result.Passed
	if slaFailed {
		level = "ERROR"
	}
	note(r.portal.Log(summaryID, level, SLAMarkdown(result), now))
	note(r.portal.Log(summaryID, "INFO", GlobalMetricsMarkdown(global), now))
	note(r.portal.Log(summaryID, "INFO", ThroughputMarkdown(throughput), now))
	note(r.portal.Log(summaryID, "INFO", PerRequestMarkdown(stats.Series()), now))

	for _, scenario := range scenarios {
		id, err := r.portal.StartRootItem("Scenario: "+scenario.name, "SUITE", scenario.first)
		note(err)
		scenario.id = id
		if err != nil || id == "" {
			continue
		}
		for _, request := range scenario.requests {
			requestID, err := r.portal.StartChildItem(id, request.label, "SUITE", request.first)
			note(err)
			request.id = requestID
			if err != nil || requestID == "" {
				continue
			}
			for _, sample := range request.reported {
				r.emitFailure(requestID, sample, note)
			}
			if request.capped {
				note(r.portal.Log(requestID, "WARN", fmt.Sprintf(
					"Failed request history capped at %d samples for '%s' in scenario '%s'. Further failures are counted in metrics only.",
					maxFailedSamplesPerRequest, request.label, scenario.name,
				), now))
			}
		}
	}

	summaryStatus := "PASSED"
	if slaFailed {
		summaryStatus = "FAILED"
	}
	note(r.portal.FinishItem(summaryID, summaryStatus, now, nil))

	for _, scenario := range scenarios {
		for _, request := range scenario.requests {
			if request.id == "" {
				continue
			}
			status := "PASSED"
			if request.failures > 0 {
				status = "FAILED"
			}
			var attrs []Attribute
			if series, ok := stats.ByName(request.label); ok {
				attrs = RequestAttributes(series)
			}
			note(r.portal.FinishItem(request.id, status, now, attrs))
		}
	}

	for _, scenario := range scenarios {
		if scenario.id == "" {
			continue
		}
		status := "PASSED"
		for _, request := range scenario.requests {
			if request.failures > 0 {
				status = "FAILED"
				break
			}
		}
		note(r.portal.FinishItem(scenario.id, status, now, nil))
	}

	note(r.portal.FinishLaunch(summaryStatus, now))
	if global.Total > 0 {
		note(r.portal.UpdateLaunch(
			SLALaunchDescription(result),
			LaunchAttributes(global, throughput, sla, result, custom),
		))
	}
	return first
}

func (r *Reporter) emitFailure(requestID string, sample Sample, note func(error)) {
	stepID, err := r.portal.StartChildItem(requestID, SampleStepName(sample), "STEP", sample.Timestamp)
	note(err)
	if err != nil || stepID == "" {
		return
	}
	note(r.portal.Log(stepID, "ERROR", FailureDetail(sample), sample.EndTime()))
	note(r.portal.FinishItem(stepID, "FAILED", sample.EndTime(), nil))
}

func (r *Reporter) trackedLabel(sample Sample) string {
	label := sample.Label
	scenario := sample.Scenario
	if scenario == "" {
		scenario = "Unknown"
	}
	key := scenario + requestKeySep + label
	if r.requestCount() < MaxTrackedNames || r.hasRequest(key) {
		return label
	}
	if !r.labelOverflow {
		r.labelOverflow = true
		warnf(r.logger, "Reached %d distinct request names; the rest are reported under '%s'.",
			MaxTrackedNames, OverflowName)
	}
	return OverflowName
}

func (r *Reporter) requestCount() int {
	n := 0
	for _, scenario := range r.scenarios {
		n += len(scenario.requests)
	}
	return n
}

func (r *Reporter) hasRequest(key string) bool {
	for _, scenario := range r.scenarios {
		if _, ok := scenario.index[key]; ok {
			return true
		}
	}
	return false
}

func (r *Reporter) remember(sample Sample, label string) {
	scenarioName := sample.Scenario
	if scenarioName == "" {
		scenarioName = "Unknown"
	}
	scenario := r.scenarioIndex[scenarioName]
	if scenario == nil {
		scenario = &scenarioNode{
			name:  scenarioName,
			first: sample.Timestamp,
			index: map[string]*requestNode{},
		}
		r.scenarioIndex[scenarioName] = scenario
		r.scenarios = append(r.scenarios, scenario)
	}
	key := scenarioName + requestKeySep + label
	request := scenario.index[key]
	if request == nil {
		request = &requestNode{
			key:      key,
			label:    label,
			scenario: scenarioName,
			first:    sample.Timestamp,
		}
		scenario.index[key] = request
		scenario.requests = append(scenario.requests, request)
	}
	if sample.Success {
		return
	}
	request.failures++
	if len(request.reported) < maxFailedSamplesPerRequest {
		request.reported = append(request.reported, sample)
		return
	}
	request.capped = true
}
