package port

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
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

// IntegrationResponse represents integration details
type IntegrationResponse struct {
	Integration struct {
		Version string `json:"version"`
	} `json:"integration"`
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

// NewClient creates a new Port API client
func NewClient(baseURL, clientID, clientSecret string) *Client {
	return &Client{
		baseURL:      strings.TrimSuffix(baseURL, "/"),
		clientID:     clientID,
		clientSecret: clientSecret,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
	}
}

// getToken returns a valid access token, refreshing if necessary
func (c *Client) getToken() (string, error) {
	now := time.Now()
	threeMinutes := 3 * time.Minute

	// Check if token is still valid for at least 3 minutes
	if c.token != "" && now.Add(threeMinutes).Before(c.tokenExpires) {
		return c.token, nil
	}

	// Authenticate
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

// GetIntegrationVersion fetches the version of an integration
func (c *Client) GetIntegrationVersion(installationID string) (string, error) {
	token, err := c.getToken()
	if err != nil {
		return "", err
	}

	req, _ := http.NewRequest(
		"GET",
		fmt.Sprintf("%s/v1/integration/%s", c.baseURL, installationID),
		nil,
	)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("integration not found")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("request failed: %s", string(body))
	}

	var intResp IntegrationResponse
	if err := json.NewDecoder(resp.Body).Decode(&intResp); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if intResp.Integration.Version == "" {
		return "", fmt.Errorf("integration version not found")
	}

	return intResp.Integration.Version, nil
}

// GetBlueprintsByDataSource fetches all blueprints for an installation
func (c *Client) GetBlueprintsByDataSource(installationID string) ([]string, error) {
	token, err := c.getToken()
	if err != nil {
		return nil, err
	}

	req, _ := http.NewRequest(
		"GET",
		fmt.Sprintf("%s/v1/data-sources", c.baseURL),
		nil,
	)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	resp, err := c.httpClient.Do(req)
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

// searchEntitiesByBlueprint searches for entities with optional query
func (c *Client) searchEntitiesByBlueprint(blueprintID string, query map[string]interface{}) ([]Entity, error) {
	token, err := c.getToken()
	if err != nil {
		return nil, err
	}

	allEntities := []Entity{}
	limit := 200
	var next string

	for {
		reqBody := map[string]interface{}{
			"limit": limit,
		}

		if query != nil {
			reqBody["query"] = query
		}

		if next != "" {
			reqBody["from"] = next
		}

		bodyBytes, _ := json.Marshal(reqBody)

		req, _ := http.NewRequest(
			"POST",
			fmt.Sprintf("%s/v1/blueprints/%s/entities/search", c.baseURL, blueprintID),
			bytes.NewReader(bodyBytes),
		)
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("request failed: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("search failed: %s", string(body))
		}

		var searchResp SearchResponse
		if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}

		allEntities = append(allEntities, searchResp.Entities...)

		if searchResp.Next == "" {
			break
		}

		next = searchResp.Next
	}

	return allEntities, nil
}

// SearchOldEntitiesByBlueprint searches for old GitHub App entities
func (c *Client) SearchOldEntitiesByBlueprint(blueprintID, oldInstallationID string) ([]Entity, error) {
	query := map[string]interface{}{
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

	return c.searchEntitiesByBlueprint(blueprintID, query)
}

// SearchNewEntitiesByBlueprint searches for new GitHub Ocean entities
func (c *Client) SearchNewEntitiesByBlueprint(blueprintID, newInstallationID string) ([]Entity, error) {
	query := map[string]interface{}{
		"combinator": "and",
		"rules": []map[string]interface{}{
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
		},
	}

	return c.searchEntitiesByBlueprint(blueprintID, query)
}

// PatchEntitiesDatasourceBulk updates entities' datasource in bulk
func (c *Client) PatchEntitiesDatasourceBulk(blueprintID string, entitiesIdentifiers []string, newDatasource string) error {
	if len(entitiesIdentifiers) == 0 {
		return nil
	}

	token, err := c.getToken()
	if err != nil {
		return err
	}

	payload := BulkPatchRequest{
		EntitiesIdentifiers: entitiesIdentifiers,
		Datasource:          newDatasource,
	}

	bodyBytes, _ := json.Marshal(payload)

	req, _ := http.NewRequest(
		"PATCH",
		fmt.Sprintf("%s/v1/blueprints/%s/datasource/bulk", c.baseURL, blueprintID),
		bytes.NewReader(bodyBytes),
	)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
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

