package models

type AuthResponse struct {
	AccessToken string `json:"accessToken"`
	ExpiresIn   int    `json:"expiresIn"`
}

type IntegrationResponse struct {
	Integration struct {
		Version string `json:"version"`
	} `json:"integration"`
}

type DataSourceResponse struct {
	DataSources []DataSource `json:"dataSources"`
}

type DataSource struct {
	Blueprints []struct {
		Identifier string `json:"identifier"`
	} `json:"blueprints"`
	Context struct {
		InstallationID string `json:"installationId"`
	} `json:"context"`
}

type SearchResponse struct {
	Entities []Entity `json:"entities"`
	Next     string   `json:"next"`
}

type EntityCountResponse struct {
	Count int `json:"count"`
}

type SearchRule struct {
	Property string `json:"property"`
	Operator string `json:"operator"`
	Value    any    `json:"value"`
}

type SearchQuery struct {
	Combinator string       `json:"combinator"`
	Rules      []SearchRule `json:"rules"`
}

type Entity struct {
	Identifier string         `json:"identifier"`
	Title      string         `json:"title,omitempty"`
	Blueprint  string         `json:"blueprint"`
	CreatedAt  string         `json:"createdAt,omitempty"`
	UpdatedAt  string         `json:"updatedAt,omitempty"`
	CreatedBy  string         `json:"createdBy,omitempty"`
	UpdatedBy  string         `json:"updatedBy,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
	Relations  map[string]any `json:"relations,omitempty"`
}

type BulkPatchRequest struct {
	EntitiesIdentifiers []string `json:"entitiesIdentifiers"`
	Datasource          string   `json:"datasource"`
}
