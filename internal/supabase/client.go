package supabase

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	managementAPIBaseURL = "https://api.supabase.com/v1"
	maxTokenBytes        = 4096
	maxResponseBytes     = 2 << 20
	requestTimeout       = 30 * time.Second
	maxRetryAfter        = time.Hour
)

var (
	ErrClient            = errors.New("Supabase API client unavailable")
	ErrRequest           = errors.New("Supabase API request failed")
	ErrUnauthorized      = errors.New("Supabase API authentication failed")
	ErrForbidden         = errors.New("Supabase API permission denied")
	ErrNotFound          = errors.New("Supabase API resource not found")
	ErrRateLimited       = errors.New("Supabase API rate limited")
	ErrServer            = errors.New("Supabase API unavailable")
	ErrRedirect          = errors.New("Supabase API redirect refused")
	ErrResponse          = errors.New("Supabase API response refused")
	ErrResponseTooLarge  = errors.New("Supabase API response exceeds size limit")
	ErrMalformedResponse = errors.New("Supabase API response malformed")
	ErrPaginationLimit   = errors.New("Supabase project listing exceeds limits")
)

// Client performs bounded, read-only requests to the fixed Management API endpoint.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// APIError reports a sanitized HTTP status and optional bounded Retry-After delay.
type APIError struct {
	StatusCode int
	RetryAfter time.Duration
	cause      error
}

func (e *APIError) Error() string {
	if e == nil || e.cause == nil {
		return ErrResponse.Error()
	}
	return e.cause.Error()
}

func (e *APIError) Unwrap() error {
	if e == nil || e.cause == nil {
		return ErrResponse
	}
	return e.cause
}

// NewClient accepts one explicitly supplied Management API token. The endpoint,
// proxy policy, redirect policy, and timeout are fixed by the client.
func NewClient(token string) (*Client, error) {
	if !validToken(token) {
		return nil, ErrClient
	}
	transport := &http.Transport{Proxy: nil, ForceAttemptHTTP2: true}
	return &Client{
		baseURL: managementAPIBaseURL,
		token:   token,
		http: &http.Client{
			Transport:     transport,
			Timeout:       requestTimeout,
			CheckRedirect: refuseRedirect,
		},
	}, nil
}

func validToken(token string) bool {
	if token == "" || len(token) > maxTokenBytes {
		return false
	}
	for i := 0; i < len(token); i++ {
		if token[i] < 0x21 || token[i] > 0x7e {
			return false
		}
	}
	return true
}

func refuseRedirect(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }

func (c *Client) get(ctx context.Context, path string, query url.Values) ([]byte, error) {
	if c == nil || c.http == nil || ctx == nil || !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "?#") {
		return nil, ErrRequest
	}
	base, err := url.Parse(c.baseURL)
	if err != nil || base.Host == "" || (base.Scheme != "https" && base.Scheme != "http") || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, ErrRequest
	}
	base.Path = strings.TrimRight(base.Path, "/") + path
	if query != nil {
		base.RawQuery = query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
	if err != nil {
		return nil, ErrRequest
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Accept", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return nil, ErrRequest
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_ = response.Body.Close()
		cause := ErrResponse
		switch response.StatusCode {
		case http.StatusUnauthorized:
			cause = ErrUnauthorized
		case http.StatusForbidden:
			cause = ErrForbidden
		case http.StatusNotFound:
			cause = ErrNotFound
		case http.StatusTooManyRequests:
			cause = ErrRateLimited
		default:
			if response.StatusCode >= http.StatusMultipleChoices && response.StatusCode < http.StatusBadRequest {
				cause = ErrRedirect
			} else if response.StatusCode >= http.StatusInternalServerError {
				cause = ErrServer
			}
		}
		return nil, &APIError{StatusCode: response.StatusCode, RetryAfter: parseRetryAfter(response.Header.Get("Retry-After"), time.Now()), cause: cause}
	}
	data, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil {
		return nil, ErrRequest
	}
	if len(data) > maxResponseBytes {
		return nil, ErrResponseTooLarge
	}
	return data, nil
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds < 0 {
			return 0
		}
		if seconds > int64(maxRetryAfter/time.Second) {
			return maxRetryAfter
		}
		return time.Duration(seconds) * time.Second
	}
	at, err := http.ParseTime(value)
	if err != nil || !at.After(now) {
		return 0
	}
	delay := at.Sub(now)
	if delay > maxRetryAfter {
		return maxRetryAfter
	}
	return delay
}
