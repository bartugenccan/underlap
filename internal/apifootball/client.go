// Package apifootball fetches match details (lineups, events, team and player
// statistics) from API-Football v3 and caches the raw responses on disk.
//
// Free plan limits, as tested on 2026-10-31: seasons 2022-2024 only, no batch
// "ids" parameter, 100 requests a day and 10 a minute. Errors come back as
// HTTP 200 with a non-empty "errors" field, so the status code alone is not
// enough.
package apifootball

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	Source  = "apifootball"
	baseURL = "https://v3.football.api-sports.io"
	// SuperLig is the league id of the Turkish Süper Lig.
	SuperLig = 203
)

type Client struct {
	key  string
	http *http.Client
	// MinGap spaces requests out to stay under the per-minute limit.
	MinGap time.Duration
	last   time.Time
	// Remaining is the daily quota left after the last request, -1 if unknown.
	Remaining int
}

func NewClient(key string) *Client {
	return &Client{key: key, http: &http.Client{Timeout: 30 * time.Second}, MinGap: 6500 * time.Millisecond, Remaining: -1}
}

// envelope is the wrapper around every response.
type envelope struct {
	Errors   json.RawMessage `json:"errors"`
	Results  int             `json:"results"`
	Response json.RawMessage `json:"response"`
}

// Get performs one request and returns the raw body after checking the error
// envelope. The raw body is what gets cached.
func (c *Client) Get(ctx context.Context, path string, params url.Values) ([]byte, error) {
	if wait := time.Until(c.last.Add(c.MinGap)); wait > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
	u := baseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-apisports-key", c.key)

	c.last = time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	if v, err := strconv.Atoi(resp.Header.Get("x-ratelimit-requests-remaining")); err == nil {
		c.Remaining = v
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: status %s", path, resp.Status)
	}
	if err := checkEnvelope(body); err != nil {
		return nil, fmt.Errorf("GET %s: %w", path, err)
	}
	return body, nil
}

// checkEnvelope returns an error when the API reported one. "errors" is [] on
// success and an object such as {"plan": "..."} on failure.
func checkEnvelope(body []byte) error {
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("decode envelope: %w", err)
	}
	e := bytes.TrimSpace(env.Errors)
	if len(e) == 0 || string(e) == "[]" || string(e) == "{}" || string(e) == "null" {
		return nil
	}
	return fmt.Errorf("api error: %s", e)
}

// decodeResponse unpacks the "response" array of a raw body.
func decodeResponse[T any](body []byte) ([]T, error) {
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, err
	}
	var out []T
	if err := json.Unmarshal(env.Response, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SeasonCode maps an API season year to the football-data style code used in
// the database: 2024 -> "2425".
func SeasonCode(year int) string { return fmt.Sprintf("%02d%02d", year%100, (year+1)%100) }
