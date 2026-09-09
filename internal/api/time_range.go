package api

import (
	"net/http"
	"time"
)

// rangeSpec describes one selectable tier of the public status page's
// history/uptime window: how far back to look (window), how wide each
// rendered bucket is (bucketWidth), and how many buckets that produces
// (bucketCount) - bucketCount * bucketWidth == window always holds (see
// design.md's Data Models).
type rangeSpec struct {
	window      time.Duration
	bucketWidth time.Duration
	bucketCount int
}

// rangeSpecs maps every valid `?range=` query value to its rangeSpec, per
// design.md's Data Models table (TRS-07). "24h" is the default tier
// (design.md's Error Handling Strategy: a missing/empty range always
// resolves here) and is byte-identical to the pre-time-range-selector fixed
// behavior (24 buckets, 1h wide).
var rangeSpecs = map[string]rangeSpec{
	"24h": {window: 24 * time.Hour, bucketWidth: 1 * time.Hour, bucketCount: 24},
	"7d":  {window: 7 * 24 * time.Hour, bucketWidth: 6 * time.Hour, bucketCount: 28},
	"30d": {window: 30 * 24 * time.Hour, bucketWidth: 24 * time.Hour, bucketCount: 30},
	"90d": {window: 90 * 24 * time.Hour, bucketWidth: 24 * time.Hour, bucketCount: 90},
}

// invalidRangeErrorBody matches the write<Foo>Error convention used
// elsewhere in this package (e.g. email_providers_handler.go).
const invalidRangeErrorBody = `{"error":"range must be one of 24h, 7d, 30d, 90d"}`

// parseRange reads the `range` query parameter from the request and
// resolves it to a rangeSpec. A missing or empty value always resolves to
// the "24h" default with ok=true (matching AD-012's spirit for an absent
// param, though - per design.md's Tech Decisions - an explicitly present
// but unrecognized value is rejected rather than clamped, since range
// selects between materially different, non-orderable views). ok is false
// only when range is present and not one of the four valid tier keys.
func parseRange(r *http.Request) (rangeSpec, bool) {
	raw := r.URL.Query().Get("range")
	if raw == "" {
		return rangeSpecs["24h"], true
	}

	spec, ok := rangeSpecs[raw]
	return spec, ok
}

// writeInvalidRangeError writes the 422 response for a present-but-invalid
// `range` query value (TRS-07), matching every other validation error in
// this package.
func writeInvalidRangeError(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnprocessableEntity)
	_, _ = w.Write([]byte(invalidRangeErrorBody))
}
