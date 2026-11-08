package memory

const DefaultPageSize = 100

type Filter struct {
	Scopes  []string `json:"scopes,omitempty"`
	IDs     []string `json:"ids,omitempty"`
	AfterID string   `json:"after_id,omitempty"`
	Limit   int      `json:"limit,omitempty"`
}

type Page struct {
	Entries []Entry `json:"entries"`
	NextID  string  `json:"next_id,omitempty"`
}
