package perf

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config is everything the output needs to open a launch. The API token is never printed.
type Config struct {
	Endpoint       string
	APIToken       string
	Project        string
	LaunchName     string
	LaunchExplicit bool
	SLA            SLAConfig
	WindowSeconds  int
	Filter         *Filter
	Attributes     []Attribute
}

func (c Config) String() string {
	return fmt.Sprintf("endpoint=%s project=%s launch=%s token=***", c.Endpoint, c.Project, c.LaunchName)
}

// Load resolves settings from the k6 --out argument, the environment, then an optional properties file.
// The first non-blank source wins, same idea as the Gatling plugin
// (system property, environment, config file).
func Load(argument string, env map[string]string, propertiesPath string) (Config, error) {
	arg := parseArgument(argument)
	environment := normalize(env)
	file, err := loadProperties(propertiesPath)
	if err != nil {
		return Config{}, err
	}

	endpoint := first(arg, environment, file, "endpoint", "rp.endpoint", "rp_endpoint")
	if endpoint == "" {
		return Config{}, fmt.Errorf("ReportPortal endpoint is required (rp.endpoint / RP_ENDPOINT / --out reportportal=endpoint=...)")
	}
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")

	token := first(arg, environment, file, "apikey", "api-key", "api_token", "token", "rp.api.key", "rp_api_key", "rp_token")
	if token == "" {
		return Config{}, fmt.Errorf("ReportPortal API token is required (rp.api.key / RP_API_KEY / RP_TOKEN)")
	}

	project := first(arg, environment, file, "project", "rp.project", "rp_project", "project_name")
	if project == "" {
		return Config{}, fmt.Errorf("ReportPortal project is required (rp.project / RP_PROJECT)")
	}

	launch, explicit := firstExplicit(arg, environment, file, "launch", "rp.launch", "rp_launch", "launch_name")
	if !explicit {
		launch = DefaultLaunchName
	}

	sla, err := parseSLA(arg, environment, file)
	if err != nil {
		return Config{}, err
	}
	windowRaw := first(arg, environment, file,
		"throughput.windowseconds", "throughput.window_seconds", "rp.throughput.window.seconds",
		"rp_throughput_window_seconds", "window", "throughput_window_seconds")
	window, err := parseWindow(windowRaw)
	if err != nil {
		return Config{}, err
	}

	include := first(arg, environment, file, "sample.includeregex", "rp.sample.include.regex", "rp_sample_include_regex", "sample_include_regex")
	exclude := first(arg, environment, file, "sample.excluderegex", "rp.sample.exclude.regex", "rp_sample_exclude_regex", "sample_exclude_regex")
	filter, err := NewFilter(include, exclude)
	if err != nil {
		return Config{}, err
	}

	return Config{
		Endpoint:       endpoint,
		APIToken:       token,
		Project:        project,
		LaunchName:     launch,
		LaunchExplicit: explicit,
		SLA:            sla,
		WindowSeconds:  window,
		Filter:         filter,
		Attributes:     parseConfigAttributes(arg, environment, file),
	}, nil
}

func parseSLA(arg, environment, file source) (SLAConfig, error) {
	p95, err := parseOptionalInt(first(arg, environment, file, "sla.p95ms", "sla.p95.ms", "rp.sla.p95.ms", "rp_sla_p95_ms"), "SLA p95")
	if err != nil {
		return SLAConfig{}, err
	}
	p99, err := parseOptionalInt(first(arg, environment, file, "sla.p99ms", "sla.p99.ms", "rp.sla.p99.ms", "rp_sla_p99_ms"), "SLA p99")
	if err != nil {
		return SLAConfig{}, err
	}
	errorRate, err := parseOptionalFloat(first(arg, environment, file, "sla.errorratepct", "sla.error.rate.pct", "rp.sla.error.rate.pct", "rp_sla_error_rate_pct"), "SLA error rate")
	if err != nil {
		return SLAConfig{}, err
	}
	minTP, err := parseOptionalFloat(first(arg, environment, file, "sla.minthroughputrps", "sla.min.throughput.rps", "rp.sla.min.throughput.rps", "rp_sla_min_throughput_rps"), "SLA min throughput")
	if err != nil {
		return SLAConfig{}, err
	}
	minPeak, err := parseOptionalFloat(first(arg, environment, file, "sla.minpeakthroughputrps", "sla.min.peak.throughput.rps", "rp.sla.min.peak.throughput.rps", "rp_sla_min_peak_throughput_rps"), "SLA min peak throughput")
	if err != nil {
		return SLAConfig{}, err
	}
	return SLAConfig{
		P95Ms:                p95,
		P99Ms:                p99,
		ErrorRatePct:         errorRate,
		MinThroughputRps:     minTP,
		MinPeakThroughputRps: minPeak,
	}, nil
}

func parseConfigAttributes(arg, environment, file source) []Attribute {
	raw := make([]string, MaxAttributes)
	any := false
	for i := 1; i <= MaxAttributes; i++ {
		n := strconv.Itoa(i)
		value := first(arg, environment, file,
			"attribute."+n,
			"attributes."+n,
			"rp.attribute."+n,
			"rp_attribute_"+n,
		)
		raw[i-1] = value
		if value != "" {
			any = true
		}
	}
	if any {
		return ParseAttributes(raw...)
	}
	combined := first(arg, environment, file, "attributes", "rp.attributes")
	if combined == "" {
		return nil
	}
	return ParseAttributes(strings.Split(combined, ",")...)
}

type source map[string]string

func (s source) get(keys ...string) string {
	if len(s) == 0 {
		return ""
	}
	for _, key := range keys {
		if value, ok := s[normalizeKey(key)]; ok {
			value = strings.TrimSpace(value)
			if value != "" {
				return value
			}
		}
	}
	return ""
}

// first returns the first non-blank value for any of keys, checking argument, then environment, then file.
func first(arg, environment, file source, keys ...string) string {
	for _, src := range []source{arg, environment, file} {
		if value := src.get(keys...); value != "" {
			return value
		}
	}
	return ""
}

func firstExplicit(arg, environment, file source, keys ...string) (string, bool) {
	for _, src := range []source{arg, environment, file} {
		if value := src.get(keys...); value != "" {
			return value, true
		}
	}
	return "", false
}

func normalize(values map[string]string) source {
	out := make(source, len(values))
	for key, value := range values {
		out[normalizeKey(key)] = value
	}
	return out
}

func normalizeKey(key string) string {
	return strings.ToLower(strings.TrimSpace(key))
}

func parseArgument(argument string) source {
	argument = strings.TrimSpace(argument)
	out := source{}
	if argument == "" {
		return out
	}
	sep := ","
	if strings.Contains(argument, ";") {
		sep = ";"
	}
	for _, part := range strings.Split(argument, sep) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		out[normalizeKey(key)] = strings.TrimSpace(value)
	}
	return out
}

func loadProperties(path string) (source, error) {
	out := source{}
	if strings.TrimSpace(path) == "" {
		return out, nil
	}
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		key, value, ok := cutProperty(line)
		if !ok {
			continue
		}
		out[normalizeKey(key)] = strings.TrimSpace(value)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return out, nil
}

func cutProperty(line string) (string, string, bool) {
	i := 0
	for i < len(line) && line[i] != '=' && line[i] != ':' && line[i] != ' ' && line[i] != '\t' {
		i++
	}
	if i == 0 || i == len(line) {
		return "", "", false
	}
	key := strings.TrimSpace(line[:i])
	rest := strings.TrimSpace(line[i:])
	rest = strings.TrimLeft(rest, "=: \t")
	if key == "" {
		return "", "", false
	}
	return key, rest, true
}

func parseWindow(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 1, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 1 {
		return 0, fmt.Errorf("throughput window size seconds must be >= 1, got: %s", raw)
	}
	return parsed, nil
}

func parseOptionalInt(raw, name string) (*int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || parsed < 0 {
		return nil, fmt.Errorf("%s threshold must be >= 0: %s", name, raw)
	}
	return &parsed, nil
}

func parseOptionalFloat(raw, name string) (*float64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil || parsed < 0 {
		return nil, fmt.Errorf("%s threshold must be >= 0: %s", name, raw)
	}
	return &parsed, nil
}
