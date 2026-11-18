package perf

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestClientLaunchLifecycle(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.Method+" "+r.URL.Path)
		mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("auth %q", r.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(r.Body)
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/launch"):
			var payload map[string]any
			_ = json.Unmarshal(body, &payload)
			if payload["name"] != "checkout" {
				t.Errorf("launch name %v", payload["name"])
			}
			w.Write([]byte(`{"id":"launch-uuid"}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/item"):
			w.Write([]byte(`{"id":"item-uuid"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/log"):
			var payload map[string]any
			_ = json.Unmarshal(body, &payload)
			if payload["level"] != "INFO" || payload["time"] == "" || payload["itemUuid"] != "item-uuid" {
				t.Errorf("log %#v", payload)
			}
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/launch/uuid/"):
			w.Write([]byte(`{"id":42,"uuid":"launch-uuid"}`))
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/launch/42/update"):
			var payload map[string]any
			_ = json.Unmarshal(body, &payload)
			if payload["description"] != "Overall: PASS" {
				t.Errorf("description %#v", payload["description"])
			}
			w.Write([]byte(`{"id":42}`))
		default:
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "secret", "my_project", nil)
	id, err := client.StartLaunch("checkout", time.Unix(1_700_000_000, 0).UTC(), []Attribute{{Key: "env", Value: "staging"}})
	if err != nil || id != "launch-uuid" {
		t.Fatalf("start %s %v", id, err)
	}
	item, err := client.StartRootItem("Performance Summary Report", "STEP", time.Now())
	if err != nil || item != "item-uuid" {
		t.Fatalf("item %s %v", item, err)
	}
	if err := client.Log(item, "INFO", "hello", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := client.FinishItem(item, "PASSED", time.Now(), []Attribute{{Key: "total", Value: "1"}}); err != nil {
		t.Fatal(err)
	}
	if err := client.FinishLaunch("PASSED", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := client.UpdateLaunch("Overall: PASS", []Attribute{{Key: "p95_ms", Value: "10"}}); err != nil {
		t.Fatal(err)
	}

	joined := strings.Join(paths, "\n")
	for _, want := range []string{
		"POST /api/v1/my_project/launch",
		"POST /api/v1/my_project/item",
		"POST /api/v1/my_project/log",
		"PUT /api/v1/my_project/item/item-uuid",
		"PUT /api/v1/my_project/launch/launch-uuid/finish",
		"GET /api/v1/my_project/launch/uuid/launch-uuid",
		"PUT /api/v1/my_project/launch/42/update",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %s in %s", want, joined)
		}
	}
}

func TestAnalyticsStartLaunchEvent(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if !strings.Contains(defaultCollectURL(), "www.google-analytics.com/mp/collect?measurement_id=") {
		t.Fatalf("collect url %s", defaultCollectURL())
	}

	dir := t.TempDir()
	agent := AgentInfo{Name: AgentName, Version: Version, FrameworkVersion: "v2.3.0"}
	analytics := &Analytics{
		enabled:      true,
		collectURL:   srv.URL,
		clientIDPath: dir + "/rp.properties",
		agent:        agent,
		http:         srv.Client(),
	}
	analytics.Start()
	analytics.Finish()

	events, _ := body["events"].([]any)
	if len(events) != 1 {
		t.Fatalf("body %#v", body)
	}
	event := events[0].(map[string]any)
	if event["name"] != "start_launch" {
		t.Fatalf("event %#v", event)
	}
	params := event["params"].(map[string]any)
	if params["agent_name"] != AgentName || params["agent_version"] != Version || params["client_name"] != ClientName || params["framework_version"] != "v2.3.0" {
		t.Fatalf("params %#v", params)
	}
	if !strings.HasPrefix(params["interpreter"].(string), "Go ") {
		t.Fatalf("interpreter %#v", params["interpreter"])
	}
	id, err := readClientID(dir + "/rp.properties")
	if err != nil || id == "" || id != body["client_id"] {
		t.Fatalf("client id %q %v body %v", id, err, body["client_id"])
	}
	again, err := readClientID(dir + "/rp.properties")
	if err != nil || again != id {
		t.Fatalf("client id not stable: %s %s %v", id, again, err)
	}

	if AnalyticsEnabled(map[string]string{DisableEnv: ""}) {
		t.Fatal("empty AGENT_NO_ANALYTICS should disable analytics")
	}
	if !AnalyticsEnabled(map[string]string{"RP_ENDPOINT": "https://rp.example.com"}) {
		t.Fatal("analytics should stay enabled")
	}

	called := false
	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer blocked.Close()
	off := &Analytics{enabled: false, collectURL: blocked.URL, http: blocked.Client()}
	off.Start()
	off.Finish()
	if called {
		t.Fatal("disabled analytics sent an event")
	}
}
