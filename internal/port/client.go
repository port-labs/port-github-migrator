package port

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	maxRetries = 3

	// MaxSearchResults caps the number of entities returned by a single
	// searchEntitiesByBlueprint call when SearchOptions.EnforceTotalLimit is true.
	MaxSearchResults = 5000
)

// Client handles all Port API interactions
type Client struct {
	baseURL        string
	clientID       string
	clientSecret   string
	httpClient     *http.Client
	token          string
	tokenExpires   time.Time
}

// AuthResponse represents the response from auth endpoint
type AuthResponse struct {
	AccessToken string `json:"accessToken"`
	ExpiresIn   int    `json:"expiresIn"`
}

// Integration represents a Port integration / installation as returned by
// GET /v1/integration/{installationId}.
type Integration struct {
	Version string         `json:"version"`
	Config  map[string]any `json:"config"`
}

// IntegrationResponse is the wire envelope returned by the integration endpoint.
type IntegrationResponse struct {
	Integration Integration `json:"integration"`
}

// DataSourceResponse represents datasources from Port
type DataSourceResponse struct {
	DataSources []DataSource `json:"dataSources"`
}

// DataSource represents a single datasource
type DataSource struct {
	Blueprints []struct {
		Identifier string `json:"identifier"`
	} `json:"blueprints"`
	Context struct {
		InstallationID string `json:"installationId"`
	} `json:"context"`
}

// SearchResponse represents entity search results
type SearchResponse struct {
	Entities []Entity `json:"entities"`
	Next     string   `json:"next"`
}

// Entity represents a Port entity
type Entity struct {
	Identifier string                 `json:"identifier"`
	Title      string                 `json:"title,omitempty"`
	Blueprint  string                 `json:"blueprint"`
	CreatedAt  string                 `json:"createdAt,omitempty"`
	UpdatedAt  string                 `json:"updatedAt,omitempty"`
	CreatedBy  string                 `json:"createdBy,omitempty"`
	UpdatedBy  string                 `json:"updatedBy,omitempty"`
	Properties map[string]interface{} `json:"properties,omitempty"`
	Relations  interface{}            `json:"relations,omitempty"`
}

// BulkPatchRequest represents a bulk patch request
type BulkPatchRequest struct {
	EntitiesIdentifiers []string `json:"entitiesIdentifiers"`
	Datasource          string   `json:"datasource"`
}

type Blueprint struct {
	Identifier string `json:"identifier"`
	Schema struct {
		Properties map[string]any `json:"properties"`
	} `json:"schema"`
	Relations map[string]any `json:"relations"`
}

type BlueprintResponse struct {
	Blueprint Blueprint `json:"blueprint"`
}

type SearchOptions struct {
	IncludeTitle bool
    IncludeProperties bool
    IncludeRelations  bool
    EnforceTotalLimit bool
}

func DefaultSearchOptions() SearchOptions {
    return SearchOptions{
        IncludeTitle: true,
        IncludeProperties: true,
        IncludeRelations:  true,
        EnforceTotalLimit: true,
    }
}

func NewClient(baseURL, clientID, clientSecret string) *Client {
	return &Client{
		baseURL:      strings.TrimSuffix(baseURL, "/"),
		clientID:     clientID,
		clientSecret: clientSecret,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) fetchAccessToken() (string, error) {
	now := time.Now()

	body := map[string]string{
		"clientId":     c.clientID,
		"clientSecret": c.clientSecret,
	}
	bodyBytes, _ := json.Marshal(body)

	resp, err := c.httpClient.Post(
		fmt.Sprintf("%s/v1/auth/access_token", c.baseURL),
		"application/json",
		bytes.NewReader(bodyBytes),
	)
	if err != nil {
		return "", fmt.Errorf("authentication request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("authentication failed: %s", string(body))
	}

	var authResp AuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return "", fmt.Errorf("failed to decode auth response: %w", err)
	}

	c.token = authResp.AccessToken
	c.tokenExpires = now.Add(time.Duration(authResp.ExpiresIn) * time.Second)

	return c.token, nil
}

func (c *Client) getToken() (string, error) {
	now := time.Now()
	threeMinutes := 3 * time.Minute

	if c.token != "" && now.Add(threeMinutes).Before(c.tokenExpires) {
		return c.token, nil
	}

	return c.fetchAccessToken()
}

func (c *Client) doAuthorizedRequest(method, url string, body []byte, contentType string) (*http.Response, error) {
	var lastErr error

	for i := 0; i <= maxRetries; i++ {
		resp, err := c.attemptWithAuth(method, url, body, contentType)
		if err != nil {
			lastErr = err
			if i == maxRetries {
				break
			}
			time.Sleep(backoff(i, 0))
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			wait := retryAfter(resp.Header.Get("x-ratelimit-reset"))
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("port returned status %d", resp.StatusCode)
			if i == maxRetries {
				break
			}
			time.Sleep(backoff(i, wait))
			continue
		}

		return resp, nil
	}

	return nil, fmt.Errorf("request failed after %d retries: %w", maxRetries, lastErr)
}

func (c *Client) attemptWithAuth(method, url string, body []byte, contentType string) (*http.Response, error) {
	do := func() (*http.Response, error) {
		var r io.Reader
		if len(body) > 0 {
			r = bytes.NewReader(body)
		}
		req, err := http.NewRequest(method, url, r)
		if err != nil {
			return nil, err
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		token, err := c.getToken()
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
		req.Header.Set("User-Agent", "port-github-migrator")
		return c.httpClient.Do(req)
	}

	resp, err := do()
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if _, err := c.fetchAccessToken(); err != nil {
			return nil, err
		}
		return do()
	}
	return resp, nil
}

// backoff returns an exponential backoff with jitter, or `hint` if it's positive
// (used to honor the server's Retry-After header).
func backoff(attempt int, hint time.Duration) time.Duration {
	if hint > 0 {
		return hint
	}
	base := time.Duration(1<<attempt) * time.Second
	jitter := time.Duration(rand.Int63n(int64(500 * time.Millisecond)))
	return base + jitter
}

func retryAfter(h string) time.Duration {
	if h == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(strings.TrimSpace(h)); err == nil {
		return time.Duration(seconds) * time.Second
	}
	if t, err := http.ParseTime(h); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// GetIntegration fetches the full integration (version + config) for the
// given installation.
func (c *Client) GetIntegration(installationID string) (*Integration, error) {
	resp, err := c.doAuthorizedRequest(
		"GET",
		fmt.Sprintf("%s/v1/integration/%s", c.baseURL, installationID),
		nil,
		"",
	)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("integration not found")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("request failed: %s", string(body))
	}

	var intResp IntegrationResponse
	if err := json.NewDecoder(resp.Body).Decode(&intResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &intResp.Integration, nil
}

// GetBlueprintsByDataSource fetches all blueprints for an installation
func (c *Client) GetBlueprintsByDataSource(installationID string) ([]string, error) {
	resp, err := c.doAuthorizedRequest(
		"GET",
		fmt.Sprintf("%s/v1/data-sources", c.baseURL),
		nil,
		"",
	)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("request failed: %s", string(body))
	}

	var dsResp DataSourceResponse
	if err := json.NewDecoder(resp.Body).Decode(&dsResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Filter datasources by installation ID
	blueprints := make(map[string]bool)
	for _, ds := range dsResp.DataSources {
		if ds.Context.InstallationID == installationID {
			for _, bp := range ds.Blueprints {
				blueprints[bp.Identifier] = true
			}
		}
	}

	if len(blueprints) == 0 {
		return nil, fmt.Errorf("no blueprints found for installation: %s", installationID)
	}

	// Convert map to slice
	result := make([]string, 0, len(blueprints))
	for bp := range blueprints {
		result = append(result, bp)
	}

	return result, nil
}

// GetBlueprint fetches a blueprint by identifier
func (c *Client) GetBlueprint(blueprintID string) (*Blueprint, error) {
	resp, err := c.doAuthorizedRequest(
		"GET",
		fmt.Sprintf("%s/v1/blueprints/%s", c.baseURL, blueprintID),
		nil,
		"",
	)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get blueprint failed: %s", string(body))
	}

	var bpResp BlueprintResponse
	if err := json.NewDecoder(resp.Body).Decode(&bpResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &bpResp.Blueprint, nil
}

func (c *Client) searchEntitiesByBlueprint(blueprintID string, query map[string]any, options *SearchOptions) ([]Entity, error) {
	allEntities := []Entity{}
	opts := DefaultSearchOptions()
	if options != nil {
		opts = *options
	}

	limitPerPage := 1000
	var next string
	searchURL := fmt.Sprintf("%s/v1/blueprints/%s/entities/search", c.baseURL, blueprintID)

	bp, err := c.GetBlueprint(blueprintID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch blueprint %s: %w", blueprintID, err)
	}

	include := []string{"$identifier"}
	if opts.IncludeTitle {
		include = append(include, "$title")
	}

	if opts.IncludeProperties {
	for k := range bp.Schema.Properties {
			include = append(include, k)
		}
	}

	if opts.IncludeRelations {
		for k := range bp.Relations {
			include = append(include, k)
		}
	}

	for {
		reqBody := map[string]any{
			"limit":   limitPerPage,
			"include": include,
		}

		if query != nil {
			reqBody["query"] = query
		}

		if next != "" {
			reqBody["from"] = next
		}

		bodyBytes, err := json.Marshal(reqBody)
		if err != nil {
			return nil, err
		}

		resp, err := c.doAuthorizedRequest("POST", searchURL, bodyBytes, "application/json")
		if err != nil {
			return nil, fmt.Errorf("request failed: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("search failed: %s", string(body))
		}

		var searchResp SearchResponse
		if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}
		resp.Body.Close()

		allEntities = append(allEntities, searchResp.Entities...)

		if searchResp.Next == "" || (opts.EnforceTotalLimit && len(allEntities) >= MaxSearchResults) {
			break
		}

		next = searchResp.Next
	}

	return allEntities, nil
}

func oldGitHubAppEntityQuery(oldInstallationID string) map[string]interface{} {
	return map[string]interface{}{
		"combinator": "and",
		"rules": []map[string]interface{}{
			{
				"property": "$datasource",
				"operator": "contains",
				"value":    "port/github/v1.0.0",
			},
			{
				"property": "$datasource",
				"operator": "contains",
				"value":    oldInstallationID,
			},
		},
	}
}

func newGitHubOceanEntityQuery(newInstallationID string, oldIdentifiers []string) map[string]any {
	rules := []map[string]any{
		{
			"property": "$datasource",
			"operator": "contains",
			"value":    "port-ocean/github-ocean",
		},
		{
			"property": "$datasource",
			"operator": "contains",
			"value":    fmt.Sprintf("%s/exporter", newInstallationID),
		},
	}

	if oldIdentifiers != nil {
		rules = append(rules, map[string]any{
			"property": "$identifier",
			"operator": "in",
			"value":    oldIdentifiers,
		})
	}

	return map[string]any{
		"combinator": "and",
		"rules":      rules,
	}
}

// SearchOldEntitiesByBlueprint searches for old GitHub App entities
func (c *Client) SearchOldEntitiesByBlueprint(blueprintID, oldInstallationID string, options *SearchOptions) ([]Entity, error) {
	return c.searchEntitiesByBlueprint(blueprintID, oldGitHubAppEntityQuery(oldInstallationID), options)
}

// SearchNewEntitiesByBlueprint searches for new GitHub Ocean entities
func (c *Client) SearchNewEntitiesByBlueprint(blueprintID, newInstallationID string, oldIdentifiers []string, options *SearchOptions) ([]Entity, error) {
	return c.searchEntitiesByBlueprint(blueprintID, newGitHubOceanEntityQuery(newInstallationID, oldIdentifiers), options)
}

// GroupCount represents a single bucket returned by the entities/group endpoint
type GroupCount struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

// GroupResponse represents the response of the entities/group endpoint
type GroupResponse struct {
	Groups        []GroupCount `json:"groups"`
	HasMoreGroups bool         `json:"hasMoreGroups"`
}

// CountEntitiesByBlueprint returns the total number of entities matching `query`
// in a blueprint, using the entities/group aggregate endpoint. This is much
// cheaper than paginating /entities/search just to count.
func (c *Client) CountEntitiesByBlueprint(blueprintID string, query map[string]interface{}) (int, error) {
	body := map[string]any{
		"query": query,
		"groupBy": map[string]any{
			"property": "$blueprint",
		},
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}

	resp, err := c.doAuthorizedRequest(
		"POST",
		fmt.Sprintf("%s/v1/blueprints/%s/entities/group", c.baseURL, blueprintID),
		bodyBytes,
		"application/json",
	)
	if err != nil {
		return 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("count failed: %s", string(b))
	}

	var gr GroupResponse
	if err := json.NewDecoder(resp.Body).Decode(&gr); err != nil {
		return 0, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(gr.Groups) == 0 {
		return 0, nil
	}
	return gr.Groups[0].Count, nil
}

// CountOldEntitiesByBlueprint counts entities ingested by the old GitHub App installation
func (c *Client) CountOldEntitiesByBlueprint(blueprintID, oldInstallationID string) (int, error) {
	return c.CountEntitiesByBlueprint(blueprintID, oldGitHubAppEntityQuery(oldInstallationID))
}

// CountNewEntitiesByBlueprint counts entities ingested by the new GitHub Ocean installation
func (c *Client) CountNewEntitiesByBlueprint(blueprintID, newInstallationID string) (int, error) {
	return c.CountEntitiesByBlueprint(blueprintID, newGitHubOceanEntityQuery(newInstallationID, nil))
}

// PatchEntitiesDatasourceBulk updates entities' datasource in bulk
func (c *Client) PatchEntitiesDatasourceBulk(blueprintID string, entitiesIdentifiers []string, newDatasource string) error {
	if len(entitiesIdentifiers) == 0 {
		return nil
	}

	payload := BulkPatchRequest{
		EntitiesIdentifiers: entitiesIdentifiers,
		Datasource:          newDatasource,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	resp, err := c.doAuthorizedRequest(
		"PATCH",
		fmt.Sprintf("%s/v1/blueprints/%s/datasource/bulk", c.baseURL, blueprintID),
		bodyBytes,
		"application/json",
	)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("patch failed: %s", string(body))
	}

	return nil
}

