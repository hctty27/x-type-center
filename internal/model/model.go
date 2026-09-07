package model

import "time"

const (
	StatusActive     = "ACTIVE"
	StatusDeprecated = "DEPRECATED"
)

type Namespace struct {
	ID          int64  `json:"id"`
	Code        string `json:"code"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	NextValue   int64  `json:"nextValue"`
	MinValue    *int64 `json:"minValue,omitempty"`
	MaxValue    *int64 `json:"maxValue,omitempty"`
	Status      string `json:"status"`
	CurrentMax  *int64 `json:"currentMax,omitempty"`
	UsedCount   int64  `json:"usedCount"`
}

type TypeEntry struct {
	ID          int64     `json:"id"`
	NamespaceID int64     `json:"namespaceId"`
	Namespace   string    `json:"namespace"`
	Value       int64     `json:"value"`
	Symbol      string    `json:"symbol,omitempty"`
	Project     string    `json:"project,omitempty"`
	Description string    `json:"description,omitempty"`
	Requirement string    `json:"requirement,omitempty"`
	Requester   string    `json:"requester,omitempty"`
	Source      string    `json:"source,omitempty"`
	SourceRef   string    `json:"sourceRef,omitempty"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type ReservedRange struct {
	ID          int64     `json:"id"`
	NamespaceID int64     `json:"namespaceId"`
	StartValue  int64     `json:"startValue"`
	EndValue    int64     `json:"endValue"`
	Project     string    `json:"project,omitempty"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

type AllocateRequest struct {
	Namespace   string `json:"namespace"`
	Project     string `json:"project"`
	Symbol      string `json:"symbol"`
	Description string `json:"description"`
	Requirement string `json:"requirement"`
	Requester   string `json:"requester"`
}

type ValidateRequest struct {
	Namespace string `json:"namespace"`
	Value     int64  `json:"value"`
	Symbol    string `json:"symbol,omitempty"`
	Project   string `json:"project,omitempty"`
}

type ValidationResult struct {
	Valid        bool       `json:"valid"`
	Exists       bool       `json:"exists"`
	SymbolMatch  bool       `json:"symbolMatch"`
	ProjectMatch bool       `json:"projectMatch"`
	Entry        *TypeEntry `json:"entry,omitempty"`
	Message      string     `json:"message,omitempty"`
}

type SearchParams struct {
	Query     string
	Namespace string
	Project   string
	Limit     int
	Offset    int
}

type SearchResult struct {
	Items []TypeEntry `json:"items"`
	Total int64       `json:"total"`
}
