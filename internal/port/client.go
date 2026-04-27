package port

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/port-labs/port-github-migrator/internal/models"
	"github.com/port-labs/port-github-migrator/internal/store"
)

type Client struct {
	baseURL       string
	clientID      string
	clientSecret  string
	httpClient    *http.Client
	token         string
	tokenExpires  time.Time
	tokenMu       sync.Mutex
	progressMu    sync.Mutex
	progressState map[string]*progressEntry
	progressOrder []string
	store         *store.Store
}

// progressEntry is the live state we keep for each in-flight blueprint search.
// Each entry owns one terminal row for its full lifetime (banner + bar on the
// same line) so concurrent searches don't fight over a single line.
type progressEntry struct {
	row            int
	installationID string
	cached         int
	hasSync        bool
	lastSync       time.Time
	total          int
	pages          int
	completed      int
	done           bool
}

func NewClient(baseURL, clientID, clientSecret string, st *store.Store) *Client {
	return &Client{
		baseURL:      strings.TrimSuffix(baseURL, "/"),
		clientID:     clientID,
		clientSecret: clientSecret,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		store:        st,
	}
}

// Store exposes the underlying cache store so callers (e.g. diff service) can
// query it directly with SQL once searches have populated it.
func (c *Client) Store() *store.Store {
	return c.store
}

// getToken returns a valid access token, refreshing if necessary
func (c *Client) getToken() (string, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	now := time.Now()
	threeMinutes := 3 * time.Minute

	if c.token != "" && now.Add(threeMinutes).Before(c.tokenExpires) {
		return c.token, nil
	}

	body := map[string]string{
		"clientId":     c.clientID,
		"clientSecret": c.clientSecret,
	}
	bodyBytes, _ := json.Marshal(body)

	var resp *http.Response
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		resp, err = c.httpClient.Post(
			fmt.Sprintf("%s/v1/auth/access_token", c.baseURL),
			"application/json",
			bytes.NewReader(bodyBytes),
		)
		if err != nil {
			return "", fmt.Errorf("authentication request failed: %w", err)
		}
		if resp.StatusCode != http.StatusTooManyRequests || attempt == 4 {
			break
		}

		wait := retryAfter(resp.Header.Get("Retry-After"), attempt)
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		time.Sleep(wait)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("authentication failed: %s", string(body))
	}

	var authResp models.AuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return "", fmt.Errorf("failed to decode auth response: %w", err)
	}

	c.token = authResp.AccessToken
	c.tokenExpires = now.Add(time.Duration(authResp.ExpiresIn) * time.Second)

	return c.token, nil
}

func (c *Client) doRequest(method, url string, body []byte, contentType string) (*http.Response, error) {
	const maxAttempts = 5

	for attempt := 0; attempt < maxAttempts; attempt++ {
		token, err := c.getToken()
		if err != nil {
			return nil, err
		}

		var reader io.Reader
		if body != nil {
			reader = bytes.NewReader(body)
		}

		req, err := http.NewRequest(method, url, reader)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("request failed: %w", err)
		}

		if resp.StatusCode != http.StatusTooManyRequests || attempt == maxAttempts-1 {
			return resp, nil
		}

		wait := retryAfter(resp.Header.Get("Retry-After"), attempt)
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		time.Sleep(wait)
	}

	return nil, fmt.Errorf("request failed after retries")
}

func retryAfter(header string, attempt int) time.Duration {
	if header != "" {
		if seconds, err := strconv.Atoi(header); err == nil {
			return time.Duration(seconds) * time.Second
		}
		if retryAt, err := http.ParseTime(header); err == nil {
			if wait := time.Until(retryAt); wait > 0 {
				return wait
			}
		}
	}

	return time.Duration(1<<attempt) * time.Second
}

// GetIntegrationVersion fetches the version of an integration
func (c *Client) GetIntegrationVersion(installationID string) (string, error) {
	resp, err := c.doRequest(
		http.MethodGet,
		fmt.Sprintf("%s/v1/integration/%s", c.baseURL, installationID),
		nil,
		"",
	)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("integration not found")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("request failed: %s", string(body))
	}

	var intResp models.IntegrationResponse
	if err := json.NewDecoder(resp.Body).Decode(&intResp); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if intResp.Integration.Version == "" {
		return "", fmt.Errorf("integration version not found")
	}

	return intResp.Integration.Version, nil
}

// GetEntityCount returns the unfiltered total entity count for a blueprint.
// The Port API doesn't accept a search query here so this is only an upper bound,
// which is fine for percentage-only progress UI (we cap it client-side).
func (c *Client) GetEntityCount(blueprintID string) (int, error) {
	resp, err := c.doRequest(
		http.MethodGet,
		fmt.Sprintf("%s/v1/blueprints/%s/entities-count", c.baseURL, blueprintID),
		nil,
		"",
	)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("entities-count failed: %s", string(body))
	}

	var countResp models.EntityCountResponse
	if err := json.NewDecoder(resp.Body).Decode(&countResp); err != nil {
		return 0, fmt.Errorf("failed to decode entities-count response: %w", err)
	}
	return countResp.Count, nil
}

// GetBlueprintsByDataSource fetches all blueprints for an installation
func (c *Client) GetBlueprintsByDataSource(installationID string) ([]string, error) {
	resp, err := c.doRequest(
		http.MethodGet,
		fmt.Sprintf("%s/v1/data-sources", c.baseURL),
		nil,
		"",
	)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("request failed: %s", string(body))
	}

	var dsResp models.DataSourceResponse
	if err := json.NewDecoder(resp.Body).Decode(&dsResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

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

	result := make([]string, 0, len(blueprints))
	for bp := range blueprints {
		result = append(result, bp)
	}

	return result, nil
}

// searchEntitiesByBlueprint searches Port for entities matching baseQuery, using the
// SQLite store as an incremental cache keyed by (blueprint, installationID).
//
// On a warm cache it issues an additional `$updatedAt > lastSyncAt` filter so we
// only fetch what changed since the previous sync, then upserts the deltas and
// returns the full merged set from the database.
func (c *Client) searchEntitiesByBlueprint(blueprintID, installationID string, baseQuery *models.SearchQuery) (_ []models.Entity, retErr error) {
	startedAt := time.Now().UTC()

	defer func() {
		if retErr != nil {
			c.failProgress(blueprintID)
		}
	}()

	lastSync, hasSync, err := c.store.GetSyncTimestamp(blueprintID, installationID)
	if err != nil {
		return nil, fmt.Errorf("read sync metadata: %w", err)
	}
	cachedCount, err := c.store.CountEntities(blueprintID, installationID)
	if err != nil {
		return nil, fmt.Errorf("count cached entities: %w", err)
	}

	searchQuery := baseQuery
	if hasSync {
		searchQuery = queryUpdatedAfter(baseQuery, lastSync)
	}

	// entities-count is unfiltered, so it's only used as a denominator hint for the
	// percentage UI. If it fails we silently fall back to an indeterminate spinner.
	totalEstimate, _ := c.GetEntityCount(blueprintID)
	c.startProgress(blueprintID, installationID, cachedCount, hasSync, lastSync, totalEstimate)

	limit := 20
	var next string
	pageCount := 0
	fetchedThisRun := 0

	for {
		reqBody := map[string]any{"limit": limit}
		if searchQuery != nil {
			reqBody["query"] = searchQuery
		}
		if next != "" {
			reqBody["from"] = next
		}

		bodyBytes, _ := json.Marshal(reqBody)
		c.renderProgress(blueprintID, pageCount+1, cachedCount+fetchedThisRun, false)

		resp, err := c.doRequest(
			http.MethodPost,
			fmt.Sprintf("%s/v1/blueprints/%s/entities/search", c.baseURL, blueprintID),
			bodyBytes,
			"application/json",
		)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("search failed: %s", string(body))
		}

		var searchResp models.SearchResponse
		if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}
		resp.Body.Close()

		if len(searchResp.Entities) > 0 {
			if err := c.store.UpsertEntities(blueprintID, installationID, searchResp.Entities); err != nil {
				return nil, fmt.Errorf("upsert entities: %w", err)
			}
		}

		fetchedThisRun += len(searchResp.Entities)
		pageCount++
		c.renderProgress(blueprintID, pageCount, cachedCount+fetchedThisRun, searchResp.Next == "")

		if searchResp.Next == "" {
			break
		}
		next = searchResp.Next
	}

	if err := c.store.SetSyncTimestamp(blueprintID, installationID, startedAt); err != nil {
		return nil, fmt.Errorf("update sync timestamp: %w", err)
	}

	return c.store.LoadEntities(blueprintID, installationID)
}

// startProgress allocates a row for `label` (if it doesn't already have one)
// and prints the initial status line — the "Fetching ..." banner and the
// progress bar live on the same line for that label. The cursor is left below
// the entire progress block so subsequent rows or unrelated output append
// naturally.
func (c *Client) startProgress(label, installationID string, cached int, hasSync bool, lastSync time.Time, total int) {
	c.progressMu.Lock()
	defer c.progressMu.Unlock()

	if c.progressState == nil {
		c.progressState = make(map[string]*progressEntry)
	}
	if _, exists := c.progressState[label]; exists {
		return
	}

	entry := &progressEntry{
		row:            len(c.progressOrder),
		installationID: installationID,
		cached:         cached,
		hasSync:        hasSync,
		lastSync:       lastSync,
		total:          total,
		completed:      cached,
	}
	c.progressState[label] = entry
	c.progressOrder = append(c.progressOrder, label)

	fmt.Fprintf(os.Stderr, "%s\n", formatProgressLine(label, entry))
}

// renderProgress updates a label's row in place using ANSI cursor positioning.
// Callers pass the dynamic counters; the static banner content was captured by
// startProgress. When every active label is done we drop the state so the next
// batch of searches starts a fresh block below.
func (c *Client) renderProgress(label string, pages, completed int, done bool) {
	c.progressMu.Lock()
	defer c.progressMu.Unlock()

	entry, ok := c.progressState[label]
	if !ok {
		return
	}
	entry.pages = pages
	entry.completed = completed
	entry.done = done

	c.redrawRowLocked(label, entry)
	c.maybeResetLocked()
}

// failProgress marks a label's row as done so the block can finalize cleanly
// when one of several concurrent searches returns an error.
func (c *Client) failProgress(label string) {
	c.progressMu.Lock()
	defer c.progressMu.Unlock()

	entry, ok := c.progressState[label]
	if !ok {
		return
	}
	entry.done = true
	c.redrawRowLocked(label, entry)
	c.maybeResetLocked()
}

// redrawRowLocked rewrites a single row of the progress block.
// Caller must hold progressMu.
//
// We assume the cursor is parked at the line immediately below the block
// (i.e. row index == len(progressOrder)). To rewrite row R we move up
// (total - R) lines, clear the line, redraw, then move back down to keep
// that invariant for the next call.
func (c *Client) redrawRowLocked(label string, entry *progressEntry) {
	total := len(c.progressOrder)
	rowsUp := total - entry.row
	line := formatProgressLine(label, entry)

	if rowsUp <= 0 {
		fmt.Fprintf(os.Stderr, "\r\033[K%s\r", line)
		return
	}
	fmt.Fprintf(os.Stderr, "\033[%dA\r\033[K%s\033[%dB\r", rowsUp, line, rowsUp)
}

// maybeResetLocked clears the progress block bookkeeping once every active
// label has finished, leaving the rendered rows in place but freeing the row
// slots so the next set of searches starts a new block below.
// Caller must hold progressMu.
func (c *Client) maybeResetLocked() {
	for _, e := range c.progressState {
		if !e.done {
			return
		}
	}
	c.progressState = nil
	c.progressOrder = nil
}

func formatProgressLine(label string, e *progressEntry) string {
	const width = 30

	prefix := fmt.Sprintf("Fetching %s entities for %s", label, e.installationID)
	if e.hasSync && e.cached > 0 {
		prefix = fmt.Sprintf("Fetching %s entities for %s (%d cached)",
			label, e.installationID, e.cached)
	}

	if e.total > 0 {
		percent := 0
		switch {
		case e.done:
			percent = 100
		case e.completed > 0:
			percent = e.completed * 100 / e.total
			if percent > 99 {
				percent = 99
			}
		}

		filled := percent * width / 100
		if filled > width {
			filled = width
		}
		bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
		return fmt.Sprintf("%s [%s] %3d%%", prefix, bar, percent)
	}

	if e.pages == 0 {
		bar := strings.Repeat("░", width)
		return fmt.Sprintf("%s [%s]", prefix, bar)
	}
	position := max((e.pages-1)%width, 0)
	bar := strings.Repeat("░", position) + "█" + strings.Repeat("░", width-position-1)
	return fmt.Sprintf("%s [%s] p%d", prefix, bar, e.pages)
}

// queryUpdatedAfter returns a copy of query with an additional `$updatedAt > ts` rule.
// We use strict `>` so we don't re-fetch entities whose updatedAt equals the previous
// sync timestamp.
func queryUpdatedAfter(query *models.SearchQuery, updatedAfter time.Time) *models.SearchQuery {
	updatedRule := models.SearchRule{
		Property: "$updatedAt",
		Operator: ">",
		Value:    updatedAfter.Format(time.RFC3339Nano),
	}

	if query == nil {
		return &models.SearchQuery{
			Combinator: "and",
			Rules:      []models.SearchRule{updatedRule},
		}
	}

	combinator := query.Combinator
	if combinator == "" {
		combinator = "and"
	}

	rules := make([]models.SearchRule, 0, len(query.Rules)+1)
	rules = append(rules, query.Rules...)
	rules = append(rules, updatedRule)

	return &models.SearchQuery{
		Combinator: combinator,
		Rules:      rules,
	}
}

// SearchOldEntitiesByBlueprint searches for old GitHub App entities
func (c *Client) SearchOldEntitiesByBlueprint(blueprintID, oldInstallationID string) ([]models.Entity, error) {
	query := &models.SearchQuery{
		Combinator: "and",
		Rules: []models.SearchRule{
			{Property: "$datasource", Operator: "contains", Value: "port/github/v1.0.0"},
			{Property: "$datasource", Operator: "contains", Value: oldInstallationID},
		},
	}

	return c.searchEntitiesByBlueprint(blueprintID, oldInstallationID, query)
}

// SearchNewEntitiesByBlueprint searches for new GitHub Ocean entities
func (c *Client) SearchNewEntitiesByBlueprint(blueprintID, newInstallationID string) ([]models.Entity, error) {
	query := &models.SearchQuery{
		Combinator: "and",
		Rules: []models.SearchRule{
			{Property: "$datasource", Operator: "contains", Value: "port-ocean/github-ocean"},
			{Property: "$datasource", Operator: "contains", Value: fmt.Sprintf("%s/exporter", newInstallationID)},
		},
	}

	return c.searchEntitiesByBlueprint(blueprintID, newInstallationID, query)
}

// PatchEntitiesDatasourceBulk updates entities' datasource in bulk.
// Cache invalidation for the patched (blueprint, installation) pair is the caller's
// responsibility because only the caller knows which old-installation rows to drop.
func (c *Client) PatchEntitiesDatasourceBulk(blueprintID string, entitiesIdentifiers []string, newDatasource string) error {
	if len(entitiesIdentifiers) == 0 {
		return nil
	}

	payload := models.BulkPatchRequest{
		EntitiesIdentifiers: entitiesIdentifiers,
		Datasource:          newDatasource,
	}

	bodyBytes, _ := json.Marshal(payload)

	resp, err := c.doRequest(
		http.MethodPatch,
		fmt.Sprintf("%s/v1/blueprints/%s/datasource/bulk", c.baseURL, blueprintID),
		bodyBytes,
		"application/json",
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("patch failed: %s", string(body))
	}

	return nil
}
