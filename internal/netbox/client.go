// Package netbox is a minimal client for the NetBox REST API.
//
// Request and response bodies are passed through unmodelled. NetBox's shapes
// are what its own API docs describe, and a duplicate set of Go structs would
// be a second source of truth that silently drops fields NetBox adds -- drift
// that is invisible, because a missing field looks like a NetBox that did not
// return one.
package netbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// maxErrorBytes caps what an error response can put into an LLM's context. A
// misconfigured proxy answers with a whole HTML page.
const maxErrorBytes = 512

// Client talks to one NetBox instance with one API token.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// New builds a client. baseURL is the NetBox root without /api.
func New(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	u := c.baseURL + "/api" + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	var rdr io.Reader

	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode body: %w", err)
		}

		rdr = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return err
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// NetBox's own scheme. Note the word Token, not Bearer: an OIDC access
	// token is not accepted here and never will be.
	req.Header.Set("Authorization", "Token "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBytes))

		return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, strings.TrimSpace(string(b)))
	}

	if resp.StatusCode == http.StatusNoContent || out == nil {
		return nil
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		if errors.Is(err, io.EOF) {
			return nil // a write that answered with no body
		}

		return fmt.Errorf("%s %s: decode: %w", method, path, err)
	}

	return nil
}

// Call performs any method against a path under /api and returns the decoded
// JSON.
func (c *Client) Call(ctx context.Context, method, path string, query url.Values, body any) (any, error) {
	var out any

	if err := c.do(ctx, method, path, query, body, &out); err != nil {
		return nil, err
	}

	if out == nil {
		// A delete answers 204. Say so, rather than returning nothing.
		return map[string]any{"ok": true}, nil
	}

	return out, nil
}

// Status returns NetBox's own version banner. It is the startup check: it
// proves the base URL resolves and the token is accepted, so a bad
// configuration fails immediately with one clear message instead of turning
// into a puzzling error on the first tool call.
//
// It deliberately does not try to work out whether the token may write.
// Nothing in the API reports that for the token actually in use, so any
// answer would be a guess -- and a wrong "yes" is worse than no claim at all.
// A token without write permission is refused by NetBox with a 403 that says
// so.
func (c *Client) Status(ctx context.Context) (string, error) {
	var status map[string]any

	if err := c.do(ctx, http.MethodGet, "/status/", nil, nil, &status); err != nil {
		return "", err
	}

	version, _ := status["netbox-version"].(string)
	if version == "" {
		return "unknown", nil
	}

	return version, nil
}
