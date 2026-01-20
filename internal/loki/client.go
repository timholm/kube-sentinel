package loki

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Client handles communication with Loki API
type Client struct {
	baseURL    string
	httpClient *http.Client
	tenantID   string
	username   string
	password   string
}

// ClientOption configures a Client
type ClientOption func(*Client)

// WithTenantID sets the X-Scope-OrgID header for multi-tenant Loki
func WithTenantID(tenantID string) ClientOption {
	return func(c *Client) {
		c.tenantID = tenantID
	}
}

// WithBasicAuth sets basic authentication credentials
func WithBasicAuth(username, password string) ClientOption {
	return func(c *Client) {
		c.username = username
		c.password = password
	}
}

// WithHTTPClient sets a custom HTTP client
func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(c *Client) {
		c.httpClient = httpClient
	}
}

// NewClient creates a new Loki client
func NewClient(baseURL string, opts ...ClientOption) *Client {
	c := &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// QueryResponse represents the response from Loki query API
type QueryResponse struct {
	Status string     `json:"status"`
	Data   QueryData  `json:"data"`
}

// QueryData holds the result data from a query
type QueryData struct {
	ResultType string   `json:"resultType"`
	Result     []Stream `json:"result"`
}

// Stream represents a log stream from Loki
type Stream struct {
	Stream map[string]string `json:"stream"`
	Values [][]string        `json:"values"` // [timestamp_ns, log_line]
}

// LogEntry represents a parsed log entry
type LogEntry struct {
	Timestamp time.Time
	Labels    map[string]string
	Line      string
}

// QueryRange executes a range query against Loki
func (c *Client) QueryRange(ctx context.Context, query string, start, end time.Time, limit int) ([]LogEntry, error) {
	params := url.Values{}
	params.Set("query", query)
	params.Set("start", strconv.FormatInt(start.UnixNano(), 10))
	params.Set("end", strconv.FormatInt(end.UnixNano(), 10))
	params.Set("limit", strconv.Itoa(limit))
	params.Set("direction", "backward")

	reqURL := fmt.Sprintf("%s/loki/api/v1/query_range?%s", c.baseURL, params.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("loki returned status %d: %s", resp.StatusCode, string(body))
	}

	var queryResp QueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&queryResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	if queryResp.Status != "success" {
		return nil, fmt.Errorf("query failed with status: %s", queryResp.Status)
	}

	return c.parseStreams(queryResp.Data.Result), nil
}

// Query executes an instant query against Loki
func (c *Client) Query(ctx context.Context, query string, at time.Time, limit int) ([]LogEntry, error) {
	params := url.Values{}
	params.Set("query", query)
	params.Set("time", strconv.FormatInt(at.UnixNano(), 10))
	params.Set("limit", strconv.Itoa(limit))
	params.Set("direction", "backward")

	reqURL := fmt.Sprintf("%s/loki/api/v1/query?%s", c.baseURL, params.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("loki returned status %d: %s", resp.StatusCode, string(body))
	}

	var queryResp QueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&queryResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	if queryResp.Status != "success" {
		return nil, fmt.Errorf("query failed with status: %s", queryResp.Status)
	}

	return c.parseStreams(queryResp.Data.Result), nil
}

// Ready checks if Loki is ready to accept requests
func (c *Client) Ready(ctx context.Context) error {
	reqURL := fmt.Sprintf("%s/ready", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("loki not ready, status: %d", resp.StatusCode)
	}

	return nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/json")

	if c.tenantID != "" {
		req.Header.Set("X-Scope-OrgID", c.tenantID)
	}

	if c.username != "" && c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}
}

func (c *Client) parseStreams(streams []Stream) []LogEntry {
	var entries []LogEntry

	for _, stream := range streams {
		for _, value := range stream.Values {
			if len(value) < 2 {
				continue
			}

			timestampNs, err := strconv.ParseInt(value[0], 10, 64)
			if err != nil {
				continue
			}

			entries = append(entries, LogEntry{
				Timestamp: time.Unix(0, timestampNs),
				Labels:    stream.Stream,
				Line:      value[1],
			})
		}
	}

	return entries
}
