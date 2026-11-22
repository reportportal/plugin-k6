package perf

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestCalculateThroughputMatchesSharedRules(t *testing.T) {
	t0 := int64(1_700_000_000_000)
	even := make([]Span, 5)
	for i := range even {
		at := t0 + int64(i)*1000
		even[i] = Span{StartMs: at, EndMs: at}
	}
	got := Calculate(even, 1)
	if got.Total != 5 || got.DurationMs != 4000 || got.FormatOverall() != "1.25" {
		t.Fatalf("overall = %+v", got)
	}

	burst := []Span{
		at(t0, 0), at(t0, 100), at(t0, 200), at(t0, 5000), at(t0, 5100),
	}
	got = Calculate(burst, 1)
	if got.FormatPeak() != "3.00" {
		t.Fatalf("peak = %s", got.FormatPeak())
	}
	if got.FormatOverall() != "0.98" {
		t.Fatalf("overall = %s", got.FormatOverall())
	}

	var packed []Span
	for i := 0; i < 10; i++ {
		packed = append(packed, at(t0, int64(i)*100))
	}
	packed = append(packed, at(t0, 20_000))
	got = Calculate(packed, 5)
	if got.FormatPeak() != "2.00" || got.FormatOverall() != "0.55" {
		t.Fatalf("5s window = %+v", got)
	}

	if empty := Calculate(nil, 1); empty.FormatOverall() != "0.00" || empty.FormatPeak() != "0.00" {
		t.Fatalf("empty = %+v", empty)
	}

	same := []Span{at(t0, 0), at(t0, 0), at(t0, 0)}
	got = Calculate(same, 1)
	if got.DurationMs != 0 || got.FormatOverall() != "0.00" || got.Total != 3 {
		t.Fatalf("zero duration = %+v", got)
	}

	var sub []Span
	for i := 0; i < 10; i++ {
		sub = append(sub, at(t0, int64(i)*50))
	}
	got = Calculate(sub, 1)
	if got.DurationMs != 450 || got.FormatOverall() != "22.22" || got.FormatPeak() != "22.22" {
		t.Fatalf("short run = %+v", got)
	}

	half := Calculate([]Span{{StartMs: t0, EndMs: t0 + 8000}}, 1)
	if half.FormatOverall() != "0.13" {
		t.Fatalf("half-up = %s", half.FormatOverall())
	}
}

func at(base, offset int64) Span {
	ts := base + offset
	return Span{StartMs: ts, EndMs: ts}
}

func TestStatsAggregateAndClip(t *testing.T) {
	c := NewCollector()
	c.Record("login", 100, true)
	c.Record("login", 300, false)
	stats, ok := c.ByName("login")
	if !ok {
		t.Fatal("missing login stats")
	}
	if stats.Total != 2 || stats.Success != 1 || stats.Failed != 1 || stats.Min != 100 || stats.Max != 300 || stats.Avg() != 200 {
		t.Fatalf("stats = %+v avg %d", stats, stats.Avg())
	}
	if stats.ErrorRate() != 50 {
		t.Fatalf("error rate %v", stats.ErrorRate())
	}

	c.Record("slow", int64(^uint64(0)>>1), true)
	slow, _ := c.ByName("slow")
	if slow.Max != 3_600_000 {
		t.Fatalf("max = %d", slow.Max)
	}

	flat := NewCollector()
	flat.Record("same", 100, true)
	flat.Record("same", 100, true)
	same, _ := flat.ByName("same")
	if p50 := same.Percentile(50); p50 < 90 || p50 > 110 {
		t.Fatalf("p50 = %d", p50)
	}

	global := c.Global()
	if global.Total != 3 {
		t.Fatalf("global total %d", global.Total)
	}
}

func TestEvaluateSLA(t *testing.T) {
	stats := Stats{Total: 1, Success: 1, Failed: 0, Min: 100, Max: 100, Sum: 100, hist: newStats("req").hist}
	stats.hist = newStats("req").hist
	_ = stats.hist.RecordValue(100)
	throughput := Throughput{OverallCents: 1234, PeakCents: 800, Total: 100, DurationMs: 8000}

	pass := Evaluate(SLAConfig{MinThroughputRps: ptrFloat(10), MinPeakThroughputRps: ptrFloat(5)}, stats, throughput)
	if !pass.Passed {
		t.Fatalf("expected pass: %+v", pass.Checks)
	}
	fail := Evaluate(SLAConfig{MinThroughputRps: ptrFloat(100)}, stats, throughput)
	if fail.Passed {
		t.Fatal("expected fail")
	}
	if !stringsContainsMetric(fail, "throughput_rps", StatusFail) {
		t.Fatalf("checks %+v", fail.Checks)
	}

	empty := Evaluate(SLAConfig{}, stats, throughput)
	if SLAMarkdown(empty) != "## SLA / Quality Gate\n\nNo SLA thresholds configured.\n" {
		t.Fatalf("markdown %q", SLAMarkdown(empty))
	}
	if SLALaunchDescription(empty) != "No SLA thresholds configured." {
		t.Fatalf("description %q", SLALaunchDescription(empty))
	}
}

func stringsContainsMetric(result SLAResult, metric string, status Status) bool {
	for _, check := range result.Checks {
		if check.Metric == metric && check.Status == status {
			return true
		}
	}
	return false
}

func ptrFloat(v float64) *float64 { return &v }

func TestParseAttributesAndFilter(t *testing.T) {
	attrs := ParseAttributes("env:staging", "release=24.3", "nightly", "", "too=many", "sixth")
	if len(attrs) != 4 {
		t.Fatalf("len %d %+v", len(attrs), attrs)
	}
	if attrs[0] != (Attribute{Key: "env", Value: "staging"}) || attrs[2] != (Attribute{Value: "nightly"}) {
		t.Fatalf("%+v", attrs)
	}
	if _, ok := parseAttribute("key:"); ok {
		t.Fatal("accepted empty value")
	}
	if _, ok := parseAttribute("   "); ok {
		t.Fatal("accepted blank")
	}

	filter, err := NewFilter(`GET /items`, `health`)
	if err != nil {
		t.Fatal(err)
	}
	if !filter.Accept("GET /items") || filter.Accept("GET /health") || filter.Accept("other") {
		t.Fatal("filter mismatch")
	}
	if _, err := NewFilter("[", ""); err == nil {
		t.Fatal("expected invalid regex")
	}
}

func TestLoadConfigOrder(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/reportportal.properties"
	properties := "rp.endpoint=https://from-file\nrp.api.key=file-token\nrp.project=file_project\nrp.launch=from file\n"
	if err := os.WriteFile(path, []byte(properties), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(
		"endpoint=https://from-arg;project=arg_project;attribute.1=env:staging;throughput.windowSizeSeconds=5",
		map[string]string{"RP_ENDPOINT": "https://from-env", "RP_API_KEY": "env-token", "RP_PROJECT": "env_project", "RP_SLA_P95_MS": "800"},
		path,
	)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Endpoint != "https://from-arg" || cfg.APIToken != "env-token" || cfg.Project != "arg_project" {
		t.Fatalf("precedence: %+v", cfg)
	}
	if cfg.LaunchName != "from file" || !cfg.LaunchExplicit {
		t.Fatalf("launch %+v", cfg)
	}
	if cfg.SLA.P95Ms == nil || *cfg.SLA.P95Ms != 800 {
		t.Fatalf("sla %+v", cfg.SLA)
	}
	if cfg.WindowSeconds != 5 {
		t.Fatalf("window %d", cfg.WindowSeconds)
	}
	if len(cfg.Attributes) != 1 || cfg.Attributes[0].Key != "env" {
		t.Fatalf("attrs %+v", cfg.Attributes)
	}
	if cfg.String() == "" || strings.Contains(cfg.String(), "env-token") {
		t.Fatalf("string leaked token: %s", cfg.String())
	}

	if _, err := Load("", map[string]string{}, ""); err == nil {
		t.Fatal("expected missing endpoint")
	}
}

func TestReporterHierarchyAndSLA(t *testing.T) {
	portal := &fakePortal{}
	p95 := int64(50)
	reporter := NewReporter(portal, SLAConfig{P95Ms: &p95}, 1, []Attribute{{Key: "env", Value: "staging"}}, nil)
	start := time.UnixMilli(1_700_000_000_000)
	if err := reporter.Init("checkout", start); err != nil {
		t.Fatal(err)
	}
	reporter.Process(Sample{
		Label: "GET /items", Scenario: "checkout", Thread: "vu-1",
		Timestamp: start, DurationMs: 40, Success: true, ResponseCode: "200",
	})
	reporter.Process(Sample{
		Label: "GET /items", Scenario: "checkout", Thread: "vu-2",
		Timestamp: start.Add(time.Second), DurationMs: 400, Success: false,
		ResponseCode: "500", ResponseMessage: "nope",
	})
	if err := reporter.Shutdown(); err != nil {
		t.Fatal(err)
	}

	if portal.launchName != "checkout" {
		t.Fatalf("launch %s", portal.launchName)
	}
	if len(portal.launchAttrs) != 1 || portal.launchAttrs[0].Value != "staging" {
		t.Fatalf("start attrs %+v", portal.launchAttrs)
	}
	if portal.summaryName != "Performance Summary Report" {
		t.Fatalf("summary %s", portal.summaryName)
	}
	var sawScenario, sawRequest, sawStep, sawWarn bool
	var requestStatus, launchStatus string
	for _, call := range portal.calls {
		switch {
		case call.op == "root" && call.name == "Scenario: checkout":
			sawScenario = true
		case call.op == "child" && call.name == "GET /items":
			sawRequest = true
		case call.op == "child" && call.name == "vu-2 | 500 | 400 ms":
			sawStep = true
		case call.op == "finish" && call.name == "GET /items":
			requestStatus = call.status
		case call.op == "finishLaunch":
			launchStatus = call.status
		}
	}
	if !sawScenario || !sawRequest || !sawStep {
		t.Fatalf("tree missing: %+v", portal.calls)
	}
	if requestStatus != "FAILED" {
		t.Fatalf("request status %s", requestStatus)
	}
	if launchStatus != "FAILED" {
		t.Fatalf("launch status %s (SLA should fail)", launchStatus)
	}
	if portal.description == "" || portal.updated["sla"] != "FAIL" || portal.updated["p95_ms"] == "" || portal.updated["throughput_rps"] == "" {
		t.Fatalf("update desc %q attrs %+v", portal.description, portal.updated)
	}
	_ = sawWarn

	passed := &fakePortal{}
	okReporter := NewReporter(passed, SLAConfig{}, 1, nil, nil)
	if err := okReporter.Init("checkout", start); err != nil {
		t.Fatal(err)
	}
	okReporter.Process(Sample{
		Label: "GET /items", Scenario: "checkout", Thread: "vu-1",
		Timestamp: start, DurationMs: 10, Success: false, ResponseCode: "500",
	})
	if err := okReporter.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if passed.launchStatus != "PASSED" {
		t.Fatalf("launch without SLA should pass, got %s", passed.launchStatus)
	}
}
