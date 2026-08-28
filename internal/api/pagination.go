package api

import (
	"net/http"
	"strconv"
)

// Page is the shared response envelope for every paginated list endpoint.
type Page[T any] struct {
	Items    []T `json:"items"`
	Total    int `json:"total"`
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
}

// parsePage reads the `page` query parameter from the request. Missing,
// non-numeric, zero, or negative values all default to page 1 rather than
// returning an error, per the spec's clamping rule.
func parsePage(r *http.Request) int {
	raw := r.URL.Query().Get("page")
	if raw == "" {
		return 1
	}

	page, err := strconv.Atoi(raw)
	if err != nil || page < 1 {
		return 1
	}

	return page
}
