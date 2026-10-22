package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// MaxSearchBody is the largest accepted search response body in bytes.
const MaxSearchBody = 1 << 20

// ErrSearchInvalidResponse marks a response that is too large or not valid JSON.
var ErrSearchInvalidResponse = errors.New("invalid response")

// SearchStatusError is returned for any non-200 reply; the body is never kept.
type SearchStatusError struct{ Code int }

func (e *SearchStatusError) Error() string { return fmt.Sprintf("HTTP %d", e.Code) }

// SearchResult is one web result as Brave sends it.
type SearchResult struct{ Title, URL, Description string }

// SearchClient talks to the Brave web search API.
type SearchClient struct {
	hc      *http.Client
	baseURL string
	apiKey  string
}

// NewSearchClient returns a client; hc must come from NewSearchHTTPClient.
func NewSearchClient(hc *http.Client, baseURL, apiKey string) *SearchClient {
	return &SearchClient{hc: hc, baseURL: baseURL, apiKey: apiKey}
}

// Search runs one web search.
func (c *SearchClient) Search(ctx context.Context, query string, count int) ([]SearchResult, error) {
	q := url.Values{"q": {query}, "count": {strconv.Itoa(count)}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/res/v1/web/search?"+q, nil)
	if err != nil {
		return nil, errors.New("search: invalid request")
	}
	req.Header.Set("X-Subscription-Token", c.apiKey)
	req.Header.Set("Accept", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &SearchStatusError{Code: resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxSearchBody+1))
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	if len(body) > MaxSearchBody {
		return nil, fmt.Errorf("search: response too large: %w", ErrSearchInvalidResponse)
	}
	var parsed struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("search: %w", ErrSearchInvalidResponse)
	}
	out := make([]SearchResult, 0, len(parsed.Web.Results))
	for _, r := range parsed.Web.Results {
		out = append(out, SearchResult{Title: r.Title, URL: r.URL, Description: r.Description})
	}
	return out, nil
}
