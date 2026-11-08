// Package client talks to an xmrgateway server.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/RogueTeam/xmrgateway/api"
)

type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// APIError is a non-2xx answer from the server.
type APIError struct {
	Status int
	Body   api.Error
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%d %s: %s", e.Status, http.StatusText(e.Status), e.Body.Message)
}

func (c *Client) do(ctx context.Context, method, path string, header http.Header, body, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, reader)
	if err != nil {
		return err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	dec := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		apiErr := &APIError{Status: resp.StatusCode}
		if dec.Decode(&apiErr.Body) != nil {
			apiErr.Body.Message = resp.Status
		}
		return apiErr
	}
	return dec.Decode(out)
}

func (c *Client) NewChallenge(ctx context.Context) (ch api.Challenge, err error) {
	err = c.do(ctx, http.MethodPost, "/api/v1/challenges", nil, nil, &ch)
	return ch, err
}

func (c *Client) Challenge(ctx context.Context, id string) (ch api.Challenge, err error) {
	err = c.do(ctx, http.MethodGet, "/api/v1/challenges/"+url.PathEscape(id), nil, nil, &ch)
	return ch, err
}

func (c *Client) Redeem(ctx context.Context, stamp string) (t api.Token, err error) {
	err = c.do(ctx, http.MethodPost, "/api/v1/tokens", nil, api.RedeemRequest{Stamp: stamp}, &t)
	return t, err
}

func (c *Client) Price(ctx context.Context, token, currency string) (p api.Price, err error) {
	header := http.Header{api.TokenHeader: {token}}
	err = c.do(ctx, http.MethodGet, "/api/v1/price?currency="+url.QueryEscape(currency), header, nil, &p)
	return p, err
}
