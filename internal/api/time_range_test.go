package api

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestRangeSpecs_BucketCountTimesBucketWidthEqualsWindow(t *testing.T) {
	for key, spec := range rangeSpecs {
		if got, want := spec.bucketWidth*time.Duration(spec.bucketCount), spec.window; got != want {
			t.Errorf("rangeSpecs[%q]: bucketWidth*bucketCount = %v, want window %v", key, got, want)
		}
	}
}

func TestRangeSpecs_MatchDesignDataModelsTable(t *testing.T) {
	cases := []struct {
		key         string
		window      time.Duration
		bucketWidth time.Duration
		bucketCount int
	}{
		{"24h", 24 * time.Hour, 1 * time.Hour, 24},
		{"7d", 7 * 24 * time.Hour, 6 * time.Hour, 28},
		{"30d", 30 * 24 * time.Hour, 24 * time.Hour, 30},
		{"90d", 90 * 24 * time.Hour, 24 * time.Hour, 90},
	}

	if len(rangeSpecs) != len(cases) {
		t.Fatalf("len(rangeSpecs) = %d, want %d", len(rangeSpecs), len(cases))
	}

	for _, c := range cases {
		spec, ok := rangeSpecs[c.key]
		if !ok {
			t.Fatalf("rangeSpecs[%q] missing", c.key)
		}
		if spec.window != c.window {
			t.Errorf("rangeSpecs[%q].window = %v, want %v", c.key, spec.window, c.window)
		}
		if spec.bucketWidth != c.bucketWidth {
			t.Errorf("rangeSpecs[%q].bucketWidth = %v, want %v", c.key, spec.bucketWidth, c.bucketWidth)
		}
		if spec.bucketCount != c.bucketCount {
			t.Errorf("rangeSpecs[%q].bucketCount = %d, want %d", c.key, spec.bucketCount, c.bucketCount)
		}
	}
}

func TestParseRange_MissingDefaultsTo24h(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)

	spec, ok := parseRange(r)
	if !ok {
		t.Fatalf("parseRange() with no ?range= ok = false, want true")
	}
	if spec != rangeSpecs["24h"] {
		t.Errorf("parseRange() with no ?range= = %+v, want %+v", spec, rangeSpecs["24h"])
	}
}

func TestParseRange_EmptyDefaultsTo24h(t *testing.T) {
	r := httptest.NewRequest("GET", "/?range=", nil)

	spec, ok := parseRange(r)
	if !ok {
		t.Fatalf("parseRange() with ?range= ok = false, want true")
	}
	if spec != rangeSpecs["24h"] {
		t.Errorf("parseRange() with ?range= = %+v, want %+v", spec, rangeSpecs["24h"])
	}
}

func TestParseRange_ValidTiersReturnMatchingSpec(t *testing.T) {
	for _, key := range []string{"24h", "7d", "30d", "90d"} {
		r := httptest.NewRequest("GET", "/?range="+key, nil)

		spec, ok := parseRange(r)
		if !ok {
			t.Fatalf("parseRange() with ?range=%s ok = false, want true", key)
		}
		if spec != rangeSpecs[key] {
			t.Errorf("parseRange() with ?range=%s = %+v, want %+v", key, spec, rangeSpecs[key])
		}
	}
}

func TestParseRange_InvalidValuesRejected(t *testing.T) {
	for _, raw := range []string{"5d", "90D", "1h", "24H", " 24h", "24h "} {
		r := httptest.NewRequest("GET", "/?range="+url.QueryEscape(raw), nil)

		if _, ok := parseRange(r); ok {
			t.Errorf("parseRange() with ?range=%q ok = true, want false", raw)
		}
	}
}

func TestWriteInvalidRangeError_WritesExpectedStatusBodyAndContentType(t *testing.T) {
	rec := httptest.NewRecorder()

	writeInvalidRangeError(rec)

	if rec.Code != 422 {
		t.Errorf("status = %d, want 422", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json")
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if want := "range must be one of 24h, 7d, 30d, 90d"; body["error"] != want {
		t.Errorf("error = %q, want %q", body["error"], want)
	}
}
