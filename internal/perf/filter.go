package perf

import (
	"fmt"
	"regexp"
	"strings"
)

// Filter drops samples by optional include and exclude label regexes.
// Exclude is applied first. An empty pattern means that side is disabled.
// Matching is unanchored, same as Java Matcher.find().
type Filter struct {
	include *regexp.Regexp
	exclude *regexp.Regexp
}

// NewFilter compiles the patterns. Invalid regexes name the setting that failed.
func NewFilter(includeRegex, excludeRegex string) (*Filter, error) {
	include, err := compileOptional(includeRegex, "sample include regex")
	if err != nil {
		return nil, err
	}
	exclude, err := compileOptional(excludeRegex, "sample exclude regex")
	if err != nil {
		return nil, err
	}
	return &Filter{include: include, exclude: exclude}, nil
}

// Accept reports whether the label should be aggregated and sent to ReportPortal.
func (f *Filter) Accept(label string) bool {
	if f == nil {
		return true
	}
	if f.exclude != nil && f.exclude.MatchString(label) {
		return false
	}
	if f.include != nil && !f.include.MatchString(label) {
		return false
	}
	return true
}

func compileOptional(regex, name string) (*regexp.Regexp, error) {
	regex = strings.TrimSpace(regex)
	if regex == "" {
		return nil, nil
	}
	re, err := regexp.Compile(regex)
	if err != nil {
		return nil, fmt.Errorf("invalid regex for %s: %s", name, regex)
	}
	return re, nil
}
