package perf

import (
	"runtime"
	"runtime/debug"
	"strings"
)

const (
	// Version is the plugin version sent as agent_version and client_version.
	Version = "1.0.0"
	// AgentName matches the @reportportal/<tool> names used by the JMeter and Gatling plugins.
	AgentName = "@reportportal/k6"
	// ClientName is the analytics client_name for this plugin.
	ClientName = "plugin-performance-k6"

	DefaultLaunchName = "k6 Performance Metrics"
)

// AgentInfo is the identity attached to the Google Analytics start_launch event.
type AgentInfo struct {
	Name             string
	Version          string
	FrameworkVersion string
}

// PluginAgent is this plugin's identity plus the k6 version it was compiled with.
func PluginAgent() AgentInfo {
	return AgentInfo{
		Name:             AgentName,
		Version:          Version,
		FrameworkVersion: FrameworkVersion(),
	}
}

// Interpreter matches the Java plugins' "Java <version>" parameter: "Go <version>".
func Interpreter() string {
	v := strings.TrimPrefix(runtime.Version(), "go")
	return "Go " + v
}

// FrameworkVersion returns the go.k6.io/k6/v2 module version linked into this binary.
func FrameworkVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	if info.Main.Path == "go.k6.io/k6/v2" && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	for _, dep := range info.Deps {
		if dep.Path != "go.k6.io/k6/v2" {
			continue
		}
		if dep.Replace != nil && dep.Replace.Version != "" && dep.Replace.Version != "(devel)" {
			return dep.Replace.Version
		}
		if dep.Version != "" && dep.Version != "(devel)" {
			return dep.Version
		}
	}
	return ""
}
