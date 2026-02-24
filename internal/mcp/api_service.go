package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// apiTimeout is the default HTTP timeout for API requests.
	apiTimeout = 60 * time.Second
	// maxResponseBody prevents unbounded memory allocation from oversized API responses.
	maxResponseBody = 10 << 20 // 10 MB
)

// APIService implements SearchService by calling Veloria's REST API.
// Used by the stdio MCP binary to communicate with a running Veloria instance.
type APIService struct {
	base   *url.URL
	client *http.Client
}

// NewAPIService creates a new APIService pointed at the given Veloria base URL.
// The URL is validated at construction time to prevent SSRF via tainted input.
func NewAPIService(baseURL string) (*APIService, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("base URL must use http or https scheme, got %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("base URL must include a host")
	}

	return &APIService{
		base: u,
		client: &http.Client{
			Timeout: apiTimeout,
		},
	}, nil
}

func (s *APIService) Search(ctx context.Context, params SearchParams) (string, *SearchResponse, error) {
	body := map[string]any{
		"term": params.Query,
		"repo": params.Repo,
	}
	if params.FileMatch != "" {
		body["file_match"] = params.FileMatch
	}
	if params.ExcludeFileMatch != "" {
		body["exclude_file_match"] = params.ExcludeFileMatch
	}
	if params.CaseSensitive {
		body["case_sensitive"] = true
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return "", nil, fmt.Errorf("failed to marshal search request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.base.JoinPath("/api/v1/search").String(), bytes.NewReader(jsonBody))
	if err != nil {
		return "", nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req) // #nosec G704 -- base URL validated in NewAPIService
	if err != nil {
		return "", nil, fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return "", nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("search failed (HTTP %d): %s", resp.StatusCode, string(data))
	}

	var apiResp apiSearchResponse
	if err := json.Unmarshal(data, &apiResp); err != nil {
		return "", nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return apiResp.ID, convertAPISearchResponse(&apiResp), nil
}

func (s *APIService) LoadSearch(ctx context.Context, searchID string) (*SearchResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		s.base.JoinPath("/api/v1/search", searchID).String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := s.client.Do(req) // #nosec G704 -- base URL validated in NewAPIService
	if err != nil {
		return nil, fmt.Errorf("load search request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search not found")
	}

	var apiResp apiSearchResponse
	if err := json.Unmarshal(data, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return convertAPISearchResponse(&apiResp), nil
}

func (s *APIService) ListExtensions(ctx context.Context, params ListParams) (*ListResponse, error) {
	endpoint := "/api/v1/" + params.Repo
	if endpoint == "/api/v1/" {
		endpoint = "/api/v1/plugins"
	}

	u := s.base.JoinPath(endpoint)

	q := u.Query()
	q.Set("page", strconv.Itoa(params.Offset/max(params.Limit, 1)+1))
	q.Set("per_page", strconv.Itoa(params.Limit))
	if params.Search != "" {
		q.Set("search", params.Search)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req) // #nosec G704 -- base URL validated in NewAPIService
	if err != nil {
		return nil, fmt.Errorf("list request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list failed (HTTP %d): %s", resp.StatusCode, string(data))
	}

	var apiResp apiListResponse
	if err := json.Unmarshal(data, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return convertAPIListResponse(&apiResp), nil
}

// API response types for deserializing Veloria's REST API responses.

type apiSearchResponse struct {
	ID              string            `json:"id"`
	Status          string            `json:"status"`
	TotalMatches    *int              `json:"total_matches"`
	TotalExtensions *int              `json:"total_extensions"`
	Results         *apiSearchResults `json:"results"`
}

type apiSearchResults struct {
	Results []*apiSearchResult `json:"results"`
	Total   int                `json:"total"`
}

type apiSearchResult struct {
	Slug           string          `json:"slug"`
	Name           string          `json:"name"`
	Version        string          `json:"version"`
	ActiveInstalls int             `json:"active_installs"`
	TotalMatches   int             `json:"total_matches"`
	Matches        []*apiFileMatch `json:"matches"`
}

type apiFileMatch struct {
	Filename string      `json:"filename"`
	Matches  []*apiMatch `json:"matches"`
}

type apiMatch struct {
	Line       string   `json:"line"`
	LineNumber int      `json:"line_number"`
	Before     []string `json:"before,omitempty"`
	After      []string `json:"after,omitempty"`
}

func convertAPISearchResponse(apiResp *apiSearchResponse) *SearchResponse {
	resp := &SearchResponse{
		Extensions: make([]ExtensionResult, 0),
	}

	if apiResp.TotalMatches != nil {
		resp.TotalMatches = *apiResp.TotalMatches
	}
	if apiResp.TotalExtensions != nil {
		resp.TotalExtensions = *apiResp.TotalExtensions
	}

	if apiResp.Results != nil {
		for _, r := range apiResp.Results.Results {
			ext := ExtensionResult{
				Slug:           r.Slug,
				Name:           r.Name,
				Version:        r.Version,
				ActiveInstalls: r.ActiveInstalls,
				TotalMatches:   r.TotalMatches,
				Matches:        make([]MatchDetail, 0),
			}

			for _, fm := range r.Matches {
				for _, m := range fm.Matches {
					ext.Matches = append(ext.Matches, MatchDetail{
						File:    fm.Filename,
						Line:    m.LineNumber,
						Content: m.Line,
						Before:  m.Before,
						After:   m.After,
					})
				}
			}

			resp.Extensions = append(resp.Extensions, ext)
		}
	}

	return resp
}

type apiListResponse struct {
	Page    int           `json:"page"`
	PerPage int           `json:"per_page"`
	Total   int64         `json:"total"`
	Results []apiListItem `json:"results"`
}

type apiListItem struct {
	Slug    string `json:"slug"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Indexed bool   `json:"indexed"`
}

func convertAPIListResponse(apiResp *apiListResponse) *ListResponse {
	resp := &ListResponse{
		Total:      int(apiResp.Total),
		Extensions: make([]ExtensionSummary, 0, len(apiResp.Results)),
	}

	for _, item := range apiResp.Results {
		resp.Extensions = append(resp.Extensions, ExtensionSummary{
			Slug:    item.Slug,
			Name:    item.Name,
			Version: item.Version,
			Indexed: item.Indexed,
		})
	}

	return resp
}
