package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestParsePage_MissingDefaultsToOne(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/incidents", nil)

	if got := parsePage(r); got != 1 {
		t.Fatalf("parsePage() with no ?page= = %d, want 1", got)
	}
}

func TestParsePage_ZeroClampsToOne(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/incidents?page=0", nil)

	if got := parsePage(r); got != 1 {
		t.Fatalf("parsePage() with ?page=0 = %d, want 1", got)
	}
}

func TestParsePage_NegativeClampsToOne(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/incidents?page=-1", nil)

	if got := parsePage(r); got != 1 {
		t.Fatalf("parsePage() with ?page=-1 = %d, want 1", got)
	}
}

func TestParsePage_NonNumericClampsToOne(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/incidents?page=abc", nil)

	if got := parsePage(r); got != 1 {
		t.Fatalf("parsePage() with ?page=abc = %d, want 1", got)
	}
}

func TestParsePage_ValidPositiveIntegerReturnsParsedValue(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/incidents?page=2", nil)

	if got := parsePage(r); got != 2 {
		t.Fatalf("parsePage() with ?page=2 = %d, want 2", got)
	}
}

func TestPage_JSONTags(t *testing.T) {
	p := Page[int]{Items: []int{1, 2}, Total: 5, Page: 1, PageSize: 2}

	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	for _, key := range []string{"items", "total", "page", "page_size"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("expected JSON key %q in encoded Page, got keys %v", key, decoded)
		}
	}
}

func TestPage_ItemsEmptyNotNilSerializesAsEmptyArray(t *testing.T) {
	p := Page[int]{Items: []int{}, Total: 0, Page: 1, PageSize: 25}

	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var decoded struct {
		Items []int `json:"items"`
	}
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if decoded.Items == nil || len(decoded.Items) != 0 {
		t.Errorf("expected items to decode as empty array, got %v", decoded.Items)
	}

	if string(b) == "" {
		t.Fatal("expected non-empty JSON output")
	}

	// Confirm the raw JSON literally contains "items":[] rather than "items":null
	if !jsonContainsEmptyItemsArray(b) {
		t.Errorf("expected raw JSON to contain an empty items array, got %s", b)
	}
}

func jsonContainsEmptyItemsArray(b []byte) bool {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return false
	}
	return string(raw["items"]) == "[]"
}
