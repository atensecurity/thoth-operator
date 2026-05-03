package thoth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultTimeout      = 30 * time.Second
	defaultRetryMax     = 4
	defaultRetryBase    = 300 * time.Millisecond
	defaultRetryMaxWait = 5 * time.Second
)

type ClientOptions struct {
	TenantID   string
	ApexDomain string
	APIBaseURL string
	AuthToken  string
	Timeout    time.Duration
}

type Client struct {
	baseURL    string
	tenantID   string
	authToken  string
	httpClient *http.Client
}

type APIError struct {
	StatusCode int
	Code       string
	Message    string
	Body       string
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	if e.Code != "" {
		return fmt.Sprintf("thoth API error (%d %s): %s", e.StatusCode, e.Code, e.Message)
	}
	if e.Message != "" {
		return fmt.Sprintf("thoth API error (%d): %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("thoth API error (%d)", e.StatusCode)
}

func NewClient(opts ClientOptions) (*Client, error) {
	tenantID := strings.TrimSpace(opts.TenantID)
	if tenantID == "" {
		return nil, errors.New("tenantId is required")
	}
	token := strings.TrimSpace(opts.AuthToken)
	if token == "" {
		return nil, errors.New("auth token is required")
	}

	baseURL := strings.TrimSpace(opts.APIBaseURL)
	if baseURL == "" {
		apex := strings.TrimSpace(opts.ApexDomain)
		if apex == "" {
			apex = "atensecurity.com"
		}
		baseURL = fmt.Sprintf("https://grid.%s.%s", tenantID, apex)
	}
	baseURL = strings.TrimRight(baseURL, "/")
	if _, err := url.Parse(baseURL); err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	return &Client{
		baseURL:    baseURL,
		tenantID:   tenantID,
		authToken:  token,
		httpClient: &http.Client{Timeout: timeout},
	}, nil
}

func (c *Client) EndpointURL() string {
	return c.baseURL
}

func (c *Client) UpdateTenantSettings(ctx context.Context, payload map[string]any) error {
	_, err := c.doJSON(ctx, http.MethodPut, c.tenantPath("settings"), nil, payload, true)
	return err
}

func (c *Client) UpsertMDMProvider(ctx context.Context, payload map[string]any) error {
	_, err := c.doJSON(ctx, http.MethodPost, c.tenantPath("mdm/providers"), nil, payload, true)
	return err
}

func (c *Client) TriggerPolicySync(ctx context.Context) error {
	_, err := c.doJSON(ctx, http.MethodPost, c.tenantPath("policies/sync"), map[string]any{}, nil, false)
	return err
}

func (c *Client) ApplyPacksBulk(ctx context.Context, payload map[string]any) error {
	_, err := c.doJSON(ctx, http.MethodPost, c.tenantPath("packs/apply"), payload, nil, false)
	return err
}

func (c *Client) BackfillGovernanceEvidence(ctx context.Context, payload map[string]any) (map[string]any, error) {
	return c.doJSON(ctx, http.MethodPost, c.governancePath("evidence/thoth/backfill"), payload, nil, false)
}

func (c *Client) BackfillGovernanceDecisionFields(ctx context.Context, payload map[string]any) (map[string]any, error) {
	return c.doJSON(ctx, http.MethodPost, c.tenantPath("governance/backfill-decision-fields"), payload, nil, false)
}

func (c *Client) tenantPath(path string) string {
	trimmed := strings.TrimPrefix(strings.TrimSpace(path), "/")
	return fmt.Sprintf("/%s/thoth/%s", c.tenantID, trimmed)
}

func (c *Client) governancePath(path string) string {
	trimmed := strings.TrimPrefix(strings.TrimSpace(path), "/")
	return fmt.Sprintf("/%s/governance/%s", c.tenantID, trimmed)
}

func (c *Client) doJSON(ctx context.Context, method, path string, payload map[string]any, out any, retryable bool) (map[string]any, error) {
	fullURL, err := c.buildURL(path)
	if err != nil {
		return nil, err
	}

	var bodyBytes []byte
	if payload != nil {
		bodyBytes, err = json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
	}

	for attempt := 1; attempt <= defaultRetryMax; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		var body io.Reader
		if bodyBytes != nil {
			body = bytes.NewReader(bodyBytes)
		}

		req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Authorization", "Bearer "+c.authToken)
		if bodyBytes != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if retryable && attempt < defaultRetryMax && isRetryableNetError(err) {
				if waitErr := waitBackoff(ctx, attempt); waitErr != nil {
					return nil, waitErr
				}
				continue
			}
			return nil, fmt.Errorf("request failed: %w", err)
		}

		respBody, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read response: %w", readErr)
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if out != nil && len(respBody) > 0 {
				if err := json.Unmarshal(respBody, out); err != nil {
					return nil, fmt.Errorf("decode response: %w", err)
				}
			}
			m := map[string]any{}
			if len(respBody) > 0 {
				_ = json.Unmarshal(respBody, &m)
			}
			return m, nil
		}

		apiErr := decodeAPIError(resp.StatusCode, respBody)
		if retryable && attempt < defaultRetryMax && isRetryableStatus(resp.StatusCode) {
			if waitErr := waitBackoff(ctx, attempt); waitErr != nil {
				return nil, waitErr
			}
			continue
		}

		return nil, apiErr
	}

	return nil, errors.New("max retry attempts exceeded")
}

func (c *Client) buildURL(path string) (string, error) {
	rel := strings.TrimSpace(path)
	if rel == "" {
		return "", errors.New("path cannot be empty")
	}
	if !strings.HasPrefix(rel, "/") {
		rel = "/" + rel
	}
	parsed, err := url.Parse(c.baseURL + rel)
	if err != nil {
		return "", fmt.Errorf("build URL: %w", err)
	}
	return parsed.String(), nil
}

func decodeAPIError(status int, body []byte) error {
	apiErr := &APIError{StatusCode: status, Body: strings.TrimSpace(string(body))}
	if len(body) == 0 {
		return apiErr
	}
	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return apiErr
	}
	if v, ok := payload["error"].(string); ok {
		apiErr.Code = v
	}
	if v, ok := payload["message"].(string); ok {
		apiErr.Message = v
	}
	if apiErr.Message == "" {
		if v, ok := payload["error_description"].(string); ok {
			apiErr.Message = v
		}
	}
	return apiErr
}

func isRetryableStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func isRetryableNetError(err error) bool {
	var nerr net.Error
	if errors.As(err, &nerr) {
		return nerr.Timeout() || nerr.Temporary()
	}
	var uerr *url.Error
	if errors.As(err, &uerr) {
		return true
	}
	return false
}

func waitBackoff(ctx context.Context, attempt int) error {
	multiplier := math.Pow(2, float64(attempt-1))
	delay := time.Duration(float64(defaultRetryBase) * multiplier)
	if delay > defaultRetryMaxWait {
		delay = defaultRetryMaxWait
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
