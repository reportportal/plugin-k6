package perf

import "strings"

// MaxAttributes is the same cap the JMeter and Gatling plugins enforce.
const MaxAttributes = 5

// Attribute is a ReportPortal launch or item attribute.
// An empty Key is a tag-style attribute (value only).
type Attribute struct {
	Key   string `json:"key,omitempty"`
	Value string `json:"value"`
}

// ParseAttributes parses up to MaxAttributes raw values.
// Accepted forms, matching the other performance plugins:
//
//	key:value
//	key=value
//	tag
//
// Blank entries are skipped. The earliest of ':' and '=' separates key and value.
func ParseAttributes(raw ...string) []Attribute {
	if len(raw) == 0 {
		return nil
	}
	limit := len(raw)
	if limit > MaxAttributes {
		limit = MaxAttributes
	}
	var out []Attribute
	for i := 0; i < limit; i++ {
		if attr, ok := parseAttribute(raw[i]); ok {
			out = append(out, attr)
		}
	}
	return out
}

func parseAttribute(raw string) (Attribute, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return Attribute{}, false
	}
	colon := indexOfSeparator(trimmed, ':')
	equals := indexOfSeparator(trimmed, '=')
	sep := pickSeparator(colon, equals)
	if sep > 0 {
		key := strings.TrimSpace(trimmed[:sep])
		value := strings.TrimSpace(trimmed[sep+1:])
		if key == "" || value == "" {
			return Attribute{}, false
		}
		return Attribute{Key: key, Value: value}, true
	}
	return Attribute{Value: trimmed}, true
}

func indexOfSeparator(s string, sep byte) int {
	idx := strings.IndexByte(s, sep)
	if idx > 0 {
		return idx
	}
	return -1
}

func pickSeparator(colon, equals int) int {
	if colon < 0 {
		return equals
	}
	if equals < 0 {
		return colon
	}
	if colon < equals {
		return colon
	}
	return equals
}
