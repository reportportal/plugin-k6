// Package reportportal is a k6 output that reports a load test to ReportPortal
// with the same launch shape as the JMeter and Gatling performance plugins.
package reportportal

import (
	"fmt"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"go.k6.io/k6/v2/metrics"
	"go.k6.io/k6/v2/output"

	"github.com/reportportal/plugin-performance-k6/internal/perf"
)

const propertiesFile = "reportportal.properties"

func init() {
	output.RegisterExtension("reportportal", New)
}

// Output streams k6 HTTP and gRPC request samples into a ReportPortal launch.
type Output struct {
	logger    logrus.FieldLogger
	config    perf.Config
	reporter  *perf.Reporter
	analytics *perf.Analytics
	pairer    *bodyPairer
}

// New validates configuration. Network calls happen in Start.
func New(params output.Params) (output.Output, error) {
	env := params.Environment
	if env == nil {
		env = environ()
	}
	cfg, err := perf.Load(params.ConfigArgument, env, propertiesFile)
	if err != nil {
		return nil, err
	}
	if !cfg.LaunchExplicit {
		if name := scriptName(params.ScriptPath); name != "" {
			cfg.LaunchName = name
		}
	}
	var logger perf.Logger
	if params.Logger != nil {
		logger = params.Logger
	}
	client := perf.NewClient(cfg.Endpoint, cfg.APIToken, cfg.Project, logger)
	return &Output{
		logger:    params.Logger,
		config:    cfg,
		reporter:  perf.NewReporter(client, cfg.SLA, cfg.WindowSeconds, cfg.Attributes, logger),
		analytics: perf.NewAnalytics(perf.AnalyticsEnabled(env), perf.PluginAgent()),
		pairer:    newBodyPairer(),
	}, nil
}

func (o *Output) Description() string {
	return fmt.Sprintf("ReportPortal (%s v%s)", perf.ClientName, perf.Version)
}

func (o *Output) Start() error {
	o.analytics.Start()
	if err := o.reporter.Init(o.config.LaunchName, time.Now()); err != nil {
		o.analytics.Finish()
		return err
	}
	if o.logger != nil {
		o.logger.Infof("ReportPortal output reporting launch %q to %s project %s", o.config.LaunchName, o.config.Endpoint, o.config.Project)
	}
	return nil
}

func (o *Output) AddMetricSamples(containers []metrics.SampleContainer) {
	for _, container := range containers {
		o.report(o.pairer.Push(container))
	}
}

// report hands samples to the reporter, after the label filter.
func (o *Output) report(samples []perf.Sample) {
	for _, sample := range samples {
		if o.config.Filter.Accept(sample.Label) {
			o.reporter.Process(sample)
		}
	}
}

func (o *Output) Stop() error {
	o.report(o.pairer.Flush())
	err := o.reporter.Shutdown()
	o.analytics.Finish()
	return err
}

func scriptName(script *url.URL) string {
	if script == nil || script.Path == "" {
		return ""
	}
	base := path.Base(script.Path)
	name := strings.TrimSuffix(base, path.Ext(base))
	if name == "" || name == "." || name == "/" {
		return ""
	}
	return name
}

func environ() map[string]string {
	out := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			out[key] = value
		}
	}
	return out
}
