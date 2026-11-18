package perf

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// DisableEnv skips the Google Analytics start_launch event when it is set to any value,
	// including empty. This is the same switch the JMeter and Gatling plugins honor.
	DisableEnv = "AGENT_NO_ANALYTICS"

	analyticsFlushTimeout = 5 * time.Second
	clientIDKey           = "client.id"
)

// Same Measurement Protocol credentials the Java performance plugins use,
// so k6 events land in the same Google Analytics property.
const encodedMeasurementCredentials = "Ry1XUDU3UlNHOFhMOjUxREVTTzQ4UV9DbmlnbVEwY2JoYmc="

// Analytics sends the anonymous start_launch event.
type Analytics struct {
	enabled      bool
	collectURL   string
	clientIDPath string
	agent        AgentInfo
	http         *http.Client
	logger       Logger

	mu   sync.Mutex
	wg   sync.WaitGroup
	sent bool
}

// AnalyticsEnabled reports whether the disable switch is absent.
// A present key, even with an empty value, disables analytics.
func AnalyticsEnabled(env map[string]string) bool {
	if env == nil {
		_, disabled := os.LookupEnv(DisableEnv)
		return !disabled
	}
	_, disabled := env[DisableEnv]
	if disabled {
		return false
	}
	for key := range env {
		if strings.EqualFold(key, DisableEnv) {
			return false
		}
	}
	return true
}

// NewAnalytics builds the sender. CollectURL and the client-id file use the shared defaults.
func NewAnalytics(enabled bool, agent AgentInfo, logger Logger) *Analytics {
	home, err := os.UserHomeDir()
	path := ""
	if err == nil && home != "" {
		path = filepath.Join(home, ".rp", "rp.properties")
	}
	return &Analytics{
		enabled:      enabled,
		collectURL:   defaultCollectURL(),
		clientIDPath: path,
		agent:        agent,
		logger:       logger,
		http: &http.Client{
			Timeout: analyticsFlushTimeout,
		},
	}
}

// Start sends the event in the background. It does not wait and never fails the run.
func (a *Analytics) Start() {
	if a == nil || !a.enabled || a.collectURL == "" {
		return
	}
	a.mu.Lock()
	if a.sent {
		a.mu.Unlock()
		return
	}
	a.sent = true
	a.wg.Add(1)
	a.mu.Unlock()

	go func() {
		defer a.wg.Done()
		a.send()
	}()
}

// Finish waits briefly for the event to leave, then returns. Analytics errors are ignored.
func (a *Analytics) Finish() {
	if a == nil || !a.enabled {
		return
	}
	done := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(analyticsFlushTimeout):
	}
}

func (a *Analytics) send() {
	body, err := json.Marshal(a.payload())
	if err != nil {
		return
	}
	req, err := http.NewRequest(http.MethodPost, a.collectURL, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.http.Do(req)
	if err != nil {
		return
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	resp.Body.Close()
}

func (a *Analytics) payload() map[string]any {
	params := map[string]string{
		"interpreter":    Interpreter(),
		"client_name":    ClientName,
		"client_version": Version,
	}
	putIfPresent(params, "agent_name", a.agent.Name)
	putIfPresent(params, "agent_version", a.agent.Version)
	putIfPresent(params, "framework_version", a.agent.FrameworkVersion)
	return map[string]any{
		"client_id": a.clientID(),
		"events": []map[string]any{
			{
				"name":   "start_launch",
				"params": params,
			},
		},
	}
}

func putIfPresent(params map[string]string, key, value string) {
	value = strings.TrimSpace(value)
	if value != "" {
		params[key] = value
	}
}

func (a *Analytics) clientID() string {
	if a.clientIDPath == "" {
		return newClientID()
	}
	id, err := readClientID(a.clientIDPath)
	if err == nil && id != "" {
		return id
	}
	id = newClientID()
	if err := writeClientID(a.clientIDPath, id); err != nil {
		return id
	}
	return id
}

func readClientID(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "!") {
			continue
		}
		key, value, ok := cutProperty(trimmed)
		if ok && key == clientIDKey {
			return strings.TrimSpace(value), nil
		}
	}
	return "", os.ErrNotExist
}

func writeClientID(path, id string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var kept []string
	if len(existing) > 0 {
		for _, line := range strings.Split(string(existing), "\n") {
			trimmed := strings.TrimSpace(line)
			key, _, ok := cutProperty(trimmed)
			if ok && key == clientIDKey {
				continue
			}
			if line == "" && len(kept) == 0 {
				continue
			}
			kept = append(kept, line)
		}
	}
	for len(kept) > 0 && strings.TrimSpace(kept[len(kept)-1]) == "" {
		kept = kept[:len(kept)-1]
	}
	kept = append(kept, clientIDKey+"="+id)
	return os.WriteFile(path, []byte(strings.Join(kept, "\n")+"\n"), 0o600)
}

func newClientID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:])
}

func defaultCollectURL() string {
	raw, err := base64.StdEncoding.DecodeString(encodedMeasurementCredentials)
	if err != nil {
		return ""
	}
	id, secret, ok := strings.Cut(string(raw), ":")
	if !ok || id == "" || secret == "" {
		return ""
	}
	return "https://www.google-analytics.com/mp/collect?measurement_id=" + id + "&api_secret=" + secret
}
