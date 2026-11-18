package perf

import (
	"fmt"
	"os"
	"time"
)

type call struct {
	op, name, status, level, message, parent, id string
}

type fakePortal struct {
	calls        []call
	seq          int
	launchName   string
	launchAttrs  []Attribute
	summaryName  string
	description  string
	updated      map[string]string
	launchStatus string
	ids          map[string]string
}

func (f *fakePortal) StartLaunch(name string, start time.Time, attributes []Attribute) (string, error) {
	f.launchName = name
	f.launchAttrs = attributes
	f.calls = append(f.calls, call{op: "launch", name: name, id: "launch"})
	return "launch", nil
}

func (f *fakePortal) StartRootItem(name, itemType string, start time.Time) (string, error) {
	f.seq++
	id := fmt.Sprintf("r%d", f.seq)
	if name == "Performance Summary Report" {
		f.summaryName = name
	}
	f.remember(id, name)
	f.calls = append(f.calls, call{op: "root", name: name, status: itemType, id: id})
	return id, nil
}

func (f *fakePortal) StartChildItem(parent, name, itemType string, start time.Time) (string, error) {
	f.seq++
	id := fmt.Sprintf("c%d", f.seq)
	f.remember(id, name)
	f.calls = append(f.calls, call{op: "child", name: name, parent: parent, status: itemType, id: id})
	return id, nil
}

func (f *fakePortal) FinishItem(id, status string, end time.Time, attributes []Attribute) error {
	f.calls = append(f.calls, call{op: "finish", name: f.ids[id], status: status, id: id})
	return nil
}

func (f *fakePortal) Log(id, level, message string, at time.Time) error {
	f.calls = append(f.calls, call{op: "log", name: f.ids[id], level: level, message: message, id: id})
	return nil
}

func (f *fakePortal) FinishLaunch(status string, end time.Time) error {
	f.launchStatus = status
	f.calls = append(f.calls, call{op: "finishLaunch", status: status})
	return nil
}

func (f *fakePortal) UpdateLaunch(description string, attributes []Attribute) error {
	f.description = description
	f.updated = map[string]string{}
	for _, attr := range attributes {
		f.updated[attr.Key] = attr.Value
	}
	return nil
}

func (f *fakePortal) remember(id, name string) {
	if f.ids == nil {
		f.ids = map[string]string{}
	}
	f.ids[id] = name
}

func writeFile(path, body string) error {
	return os.WriteFile(path, []byte(body), 0o644)
}
