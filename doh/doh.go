// Package doh is the library behind the doh command line:
// the HTTP client, request shaping, and the typed data models for DNS over HTTPS.
//
// The Client here is the spine every command shares. It sets a real
// User-Agent, paces requests so a busy session stays polite, and retries the
// transient failures (429 and 5xx) that any public API throws under load.
// DNS queries are made to Google's public DoH API (dns.google/resolve).
package doh

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultUserAgent identifies the client to dns.google.
const DefaultUserAgent = "doh/dev (+https://github.com/tamnd/doh-cli)"

// Host is the DNS-over-HTTPS resolver host.
const Host = "dns.google"

// BaseURL is the root every request is built from.
const BaseURL = "https://" + Host

// typeNames maps DNS record type numbers to their string names.
var typeNames = map[int]string{
	1:   "A",
	2:   "NS",
	5:   "CNAME",
	6:   "SOA",
	12:  "PTR",
	15:  "MX",
	16:  "TXT",
	28:  "AAAA",
	33:  "SRV",
	255: "ANY",
}

// statusNames maps DNS RCODE values to human-readable names.
var statusNames = map[int]string{
	0: "NOERROR",
	1: "FORMERR",
	2: "SERVFAIL",
	3: "NXDOMAIN",
	4: "NOTIMP",
	5: "REFUSED",
}

// Config holds per-client tunables.
type Config struct {
	BaseURL string
	Rate    time.Duration
	Retries int
	Timeout time.Duration
}

// DefaultConfig returns sensible defaults for the DoH client.
func DefaultConfig() Config {
	return Config{
		BaseURL: BaseURL,
		Rate:    0,
		Retries: 3,
		Timeout: 10 * time.Second,
	}
}

// Record is one DNS resource record returned by a resolve call.
type Record struct {
	Name string `json:"name"`
	Type string `json:"type"` // "A", "AAAA", "MX", "TXT", etc.
	TTL  int    `json:"ttl"`
	Data string `json:"data"`
}

// wire types for JSON decoding

type wireResponse struct {
	Status    int           `json:"Status"`
	Question  []wireQuestion `json:"Question"`
	Answer    []wireRecord  `json:"Answer"`
	Authority []wireRecord  `json:"Authority"`
}

type wireQuestion struct {
	Name string `json:"name"`
	Type int    `json:"type"`
}

type wireRecord struct {
	Name string `json:"name"`
	Type int    `json:"type"`
	TTL  int    `json:"TTL"`
	Data string `json:"data"`
}

// Client talks to dns.google over HTTPS.
type Client struct {
	HTTP      *http.Client
	UserAgent string
	BaseURL   string
	// Rate is the minimum gap between requests. Zero means no pacing.
	Rate    time.Duration
	Retries int

	last time.Time
}

// NewClient returns a Client with sensible defaults.
func NewClient() *Client {
	cfg := DefaultConfig()
	return &Client{
		HTTP:      &http.Client{Timeout: cfg.Timeout},
		UserAgent: DefaultUserAgent,
		BaseURL:   cfg.BaseURL,
		Rate:      cfg.Rate,
		Retries:   cfg.Retries,
	}
}

// Resolve queries dns.google for the given domain and record type (e.g. "A", "MX").
// It returns all answer and authority records, or an error if the DNS status is non-zero.
func (c *Client) Resolve(ctx context.Context, domain, recordType string) ([]Record, error) {
	url := c.BaseURL + "/resolve?name=" + domain + "&type=" + recordType
	body, err := c.Get(ctx, url)
	if err != nil {
		return nil, err
	}

	var wr wireResponse
	if err := json.Unmarshal(body, &wr); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if wr.Status != 0 {
		name, ok := statusNames[wr.Status]
		if !ok {
			name = fmt.Sprintf("status %d", wr.Status)
		}
		return nil, fmt.Errorf("DNS error: %s (%d)", name, wr.Status)
	}

	all := append(wr.Answer, wr.Authority...)
	records := make([]Record, 0, len(all))
	for _, w := range all {
		typeName, ok := typeNames[w.Type]
		if !ok {
			typeName = fmt.Sprintf("TYPE%d", w.Type)
		}
		records = append(records, Record{
			Name: w.Name,
			Type: typeName,
			TTL:  w.TTL,
			Data: w.Data,
		})
	}
	return records, nil
}

// Get fetches url and returns the response body. It paces and retries according
// to the client's settings. The caller owns nothing extra; the body is read
// fully and closed here.
func (c *Client) Get(ctx context.Context, url string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, url)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", url, lastErr)
}

func (c *Client) do(ctx context.Context, url string) (body []byte, retry bool, err error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.UserAgent)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

// pace blocks until at least Rate has passed since the previous request.
func (c *Client) pace() {
	if c.Rate <= 0 {
		return
	}
	if wait := c.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}
