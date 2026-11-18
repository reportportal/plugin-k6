package perf

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Portal is the ReportPortal API surface the reporter uses.
type Portal interface {
	StartLaunch(name string, start time.Time, attributes []Attribute) (string, error)
	StartRootItem(name, itemType string, start time.Time) (string, error)
	StartChildItem(parentUUID, name, itemType string, start time.Time) (string, error)
	FinishItem(itemUUID, status string, end time.Time, attributes []Attribute) error
	Log(itemUUID, level, message string, at time.Time) error
	FinishLaunch(status string, end time.Time) error
	UpdateLaunch(description string, attributes []Attribute) error
}

// Client talks to the ReportPortal 5.x HTTP API the same way client-java does:
// API key, no cookies, paths under /api/v1/{project}/.
type Client struct {
	endpoint   string
	token      string
	project    string
	http       *http.Client
	logger     Logger
	launchUUID string
	launchDBID int64
}

func NewClient(endpoint, token, project string, logger Logger) *Client {
	return &Client{
		endpoint: strings.TrimRight(strings.TrimSpace(endpoint), "/"),
		token:    token,
		project:  project,
		logger:   logger,
		http: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (c *Client) StartLaunch(name string, start time.Time, attributes []Attribute) (string, error) {
	warnIfPlainHTTP(c.logger, c.endpoint)
	infof(c.logger, "Initializing ReportPortal client for project %s", c.project)

	body := map[string]any{
		"name":      name,
		"startTime": FormatTime(start),
	}
	if len(attributes) > 0 {
		body["attributes"] = attributes
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := c.send(http.MethodPost, c.projectPath("launch"), body, &created); err != nil {
		return "", err
	}
	if created.ID == "" {
		return "", fmt.Errorf("ReportPortal start launch returned an empty id")
	}
	c.launchUUID = created.ID
	return created.ID, nil
}

func (c *Client) StartRootItem(name, itemType string, start time.Time) (string, error) {
	return c.startItem("", name, itemType, start)
}

func (c *Client) StartChildItem(parentUUID, name, itemType string, start time.Time) (string, error) {
	return c.startItem(parentUUID, name, itemType, start)
}

func (c *Client) startItem(parentUUID, name, itemType string, start time.Time) (string, error) {
	body := map[string]any{
		"name":       name,
		"startTime":  FormatTime(start),
		"type":       itemType,
		"launchUuid": c.launchUUID,
	}
	path := c.projectPath("item")
	if parentUUID != "" {
		path = c.projectPath("item/" + url.PathEscape(parentUUID))
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := c.send(http.MethodPost, path, body, &created); err != nil {
		return "", err
	}
	if created.ID == "" {
		return "", fmt.Errorf("ReportPortal start item %q returned an empty id", name)
	}
	return created.ID, nil
}

func (c *Client) FinishItem(itemUUID, status string, end time.Time, attributes []Attribute) error {
	body := map[string]any{
		"endTime":    FormatTime(end),
		"status":     status,
		"launchUuid": c.launchUUID,
	}
	if len(attributes) > 0 {
		body["attributes"] = attributes
	}
	return c.send(http.MethodPut, c.projectPath("item/"+url.PathEscape(itemUUID)), body, nil)
}

func (c *Client) Log(itemUUID, level, message string, at time.Time) error {
	body := map[string]any{
		"launchUuid": c.launchUUID,
		"itemUuid":   itemUUID,
		"time":       FormatTime(at),
		"message":    message,
		"level":      level,
	}
	return c.send(http.MethodPost, c.projectPath("log"), body, nil)
}

func (c *Client) FinishLaunch(status string, end time.Time) error {
	infof(c.logger, "Finishing ReportPortal launch")
	body := map[string]any{
		"endTime": FormatTime(end),
		"status":  status,
	}
	err := c.send(http.MethodPut, c.projectPath("launch/"+url.PathEscape(c.launchUUID)+"/finish"), body, nil)
	if err == nil {
		infof(c.logger, "ReportPortal launch finished")
	}
	return err
}

func (c *Client) UpdateLaunch(description string, attributes []Attribute) error {
	infof(c.logger, "Updating launch attributes with performance metrics and SLA results")
	id, err := c.numericLaunchID()
	if err != nil {
		errorf(c.logger, "Failed to retrieve launch from server; attributes update skipped: %v", err)
		return nil
	}
	body := map[string]any{
		"description": description,
	}
	if attributes != nil {
		body["attributes"] = attributes
	}
	if err := c.send(http.MethodPut, c.projectPath("launch/"+url.PathEscape(id)+"/update"), body, nil); err != nil {
		errorf(c.logger, "ReportPortal updateLaunch rejected request: %v", err)
		return nil
	}
	infof(c.logger, "Updated launch description and attributes with performance metrics and SLA results")
	return nil
}

func (c *Client) numericLaunchID() (string, error) {
	if c.launchDBID > 0 {
		return fmt.Sprintf("%d", c.launchDBID), nil
	}
	var resource struct {
		ID int64 `json:"id"`
	}
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(200 * time.Millisecond)
		}
		last = c.send(http.MethodGet, c.projectPath("launch/uuid/"+url.PathEscape(c.launchUUID)), nil, &resource)
		if last == nil && resource.ID > 0 {
			c.launchDBID = resource.ID
			return fmt.Sprintf("%d", resource.ID), nil
		}
	}
	if last == nil {
		last = fmt.Errorf("launch uuid %s has no numeric id", c.launchUUID)
	}
	return "", last
}

func (c *Client) projectPath(suffix string) string {
	return "v1/" + url.PathEscape(c.project) + "/" + suffix
}

func (c *Client) send(method, path string, body any, dest any) error {
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	endpoint := c.endpoint + "/api/" + path

	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(200 * time.Millisecond)
		}
		var reader io.Reader
		if payload != nil {
			reader = bytes.NewReader(payload)
		}
		req, err := http.NewRequest(method, endpoint, reader)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Accept", "application/json")
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.http.Do(req)
		if err != nil {
			last = err
			continue
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if readErr != nil {
			last = readErr
			continue
		}
		if resp.StatusCode >= 300 {
			return fmt.Errorf("ReportPortal %s %s returned HTTP %d: %s", method, path, resp.StatusCode, trimBody(data))
		}
		if dest != nil && len(bytes.TrimSpace(data)) > 0 {
			if err := json.Unmarshal(data, dest); err != nil {
				return fmt.Errorf("decode ReportPortal response: %w", err)
			}
		}
		return nil
	}
	if last == nil {
		last = fmt.Errorf("ReportPortal %s %s failed", method, path)
	}
	return last
}

func trimBody(data []byte) string {
	text := strings.TrimSpace(string(data))
	if len(text) > 500 {
		return text[:500]
	}
	if text == "" {
		return "empty response body"
	}
	return text
}

// FormatTime renders the UTC timestamp ReportPortal stores (millisecond precision).
func FormatTime(t time.Time) string {
	if t.IsZero() {
		t = time.Now()
	}
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

func warnIfPlainHTTP(logger Logger, endpoint string) {
	if len(endpoint) >= 7 && strings.EqualFold(endpoint[:7], "http://") {
		warnf(logger, "ReportPortal endpoint %s uses plain HTTP, so the API token is sent unencrypted. Prefer https://", endpoint)
	}
}
