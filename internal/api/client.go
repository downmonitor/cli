// Package api is the HTTP client for the Downmonitor API.
//
// It only transports requests and responses. Verdicts, percentiles and
// assertion results are computed by the server and passed through unchanged,
// so the CLI output always matches what the API returns.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/downmonitor/cli/internal/exitcode"
)

// ContractVersion is the dated API version this client was written against.
// It is sent with every request so the server keeps answering with the same
// response shapes after it evolves.
const ContractVersion = "2026-09-01"

// Version is the CLI version reported in the User-Agent header. It is set at
// build time.
var Version = "dev"

// maxResponseSize bounds how much of a response body is read into memory.
const maxResponseSize = 16 << 20

// defaultTimeout is generous on purpose: a check fans out to many regions and
// the server waits for the slowest one before answering.
const defaultTimeout = 120 * time.Second

// Client talks to one Downmonitor API endpoint with one credential.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// New returns a client for baseURL, authenticating with token when it is not
// empty.
func New(baseURL, token string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		HTTP:    &http.Client{Timeout: defaultTimeout},
	}
}

// Check runs a check (POST /checks).
func (c *Client) Check(ctx context.Context, body CheckRequest) (*Check, error) {
	var check Check
	if err := c.do(ctx, http.MethodPost, "/checks", body, &check); err != nil {
		return nil, err
	}

	return &check, nil
}

// FetchCheck returns a previous check by id (GET /checks/{id}). It is not
// billed.
func (c *Client) FetchCheck(ctx context.Context, id string) (*Check, error) {
	var check Check
	if err := c.do(ctx, http.MethodGet, "/checks/"+url.PathEscape(id), nil, &check); err != nil {
		return nil, err
	}

	return &check, nil
}

// Regions lists the regions currently in service (GET /regions).
func (c *Client) Regions(ctx context.Context) (*Regions, error) {
	var regions Regions
	if err := c.do(ctx, http.MethodGet, "/regions", nil, &regions); err != nil {
		return nil, err
	}

	return &regions, nil
}

// Me describes the credential in use (GET /me).
func (c *Client) Me(ctx context.Context) (*Me, error) {
	var me Me
	if err := c.do(ctx, http.MethodGet, "/me", nil, &me); err != nil {
		return nil, err
	}

	return &me, nil
}

// StartDeviceLogin starts an OAuth 2.0 device authorization (RFC 8628). The
// request is unauthenticated.
func (c *Client) StartDeviceLogin(ctx context.Context, tokenType, clientName string) (*DeviceAuthorization, error) {
	var authorization DeviceAuthorization

	body := map[string]string{"type": tokenType, "client_name": clientName}
	if err := c.do(ctx, http.MethodPost, "/auth/device", body, &authorization); err != nil {
		return nil, err
	}

	return &authorization, nil
}

// PollDeviceLogin polls a pending device authorization. While the user has not
// approved it, the returned *Error carries one of the RFC 8628 codes:
// authorization_pending, slow_down, access_denied or expired_token.
func (c *Client) PollDeviceLogin(ctx context.Context, deviceCode string) (*CollectedToken, error) {
	var collected CollectedToken

	body := map[string]string{"device_code": deviceCode}
	if err := c.do(ctx, http.MethodPost, "/auth/device/token", body, &collected); err != nil {
		return nil, err
	}

	return &collected, nil
}

func (c *Client) do(ctx context.Context, method, path string, body, into any) error {
	var reader io.Reader

	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return exitcode.Usagef("could not encode the request: %w", err)
		}

		reader = bytes.NewReader(encoded)
	}

	request, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return exitcode.Usagef("invalid API URL %q: %w", c.BaseURL, err)
	}

	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Api-Version", ContractVersion)
	request.Header.Set("User-Agent", "downmonitor/"+Version)

	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	if c.Token != "" {
		request.Header.Set("Authorization", "Bearer "+c.Token)
	}

	response, err := c.HTTP.Do(request)
	if err != nil {
		return transportError(c.BaseURL, err)
	}
	defer response.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(response.Body, maxResponseSize))
	if err != nil {
		return exitcode.Unreachablef("could not read the API response: %w", err)
	}

	if response.StatusCode >= http.StatusBadRequest {
		return parseError(response, payload)
	}

	if into == nil {
		return nil
	}

	if err := json.Unmarshal(payload, into); err != nil {
		return exitcode.Unreachablef("unexpected API response (is this CLI up to date?): %w", err)
	}

	return nil
}

// transportError maps a failed round trip to Unreachable: the API was never
// reached, so nothing is known about the target.
func transportError(baseURL string, err error) error {
	if errors.Is(err, context.Canceled) {
		return exitcode.Unreachablef("interrupted")
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return exitcode.Unreachablef("the API did not answer in time")
	}

	return exitcode.Unreachablef("could not reach %s: %w", baseURL, err)
}

// parseError decodes the API error envelope. A body that is not the envelope
// (an HTML page from a proxy, for instance) still produces an *Error, since the
// status code alone is enough to choose an exit code.
func parseError(response *http.Response, payload []byte) error {
	apiErr := &Error{Status: response.StatusCode}

	if err := json.Unmarshal(payload, apiErr); err != nil || apiErr.Code == "" {
		apiErr.Code = "http_" + strconv.Itoa(response.StatusCode)
		apiErr.Message = fmt.Sprintf("the API answered %s", response.Status)
	}

	var extra map[string]any
	if json.Unmarshal(payload, &extra) == nil {
		delete(extra, "error")
		delete(extra, "message")
		delete(extra, "errors")
		apiErr.Extra = extra
	}

	if after := response.Header.Get("Retry-After"); after != "" {
		if seconds, err := strconv.Atoi(after); err == nil && seconds > 0 {
			apiErr.RetryAfter = seconds
		}
	}

	return apiErr
}
