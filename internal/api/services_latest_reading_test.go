package api

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

func float64Ptr(v float64) *float64 { return &v }

func int64Ptr(v int64) *int64 { return &v }

func timePtr(v time.Time) *time.Time { return &v }

// serviceWithReading returns a db.Service carrying a complete reading
// (every column UpdateStatusAndReading writes).
func serviceWithReading(short, long float64, errors, requests int64, readAt time.Time) *db.Service {
	return &db.Service{
		ID:                 "svc-1",
		LastBurnRateShort:  float64Ptr(short),
		LastBurnRateLong:   float64Ptr(long),
		LastWindowErrors:   int64Ptr(errors),
		LastWindowRequests: int64Ptr(requests),
		LastReadingAt:      timePtr(readAt),
	}
}

// TestToLatestReadingResponse_CompleteReading_MapsAllFields covers BURN-14:
// a complete persisted reading maps 1:1 into the response.
func TestToLatestReadingResponse_CompleteReading_MapsAllFields(t *testing.T) {
	readAt := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	got := toLatestReadingResponse(serviceWithReading(12.3, 8.1, 5, 120, readAt))

	if got == nil {
		t.Fatal("toLatestReadingResponse() = nil, want a reading")
	}
	if got.BurnRateShort == nil || *got.BurnRateShort != 12.3 {
		t.Errorf("BurnRateShort = %v, want 12.3", got.BurnRateShort)
	}
	if got.BurnRateLong == nil || *got.BurnRateLong != 8.1 {
		t.Errorf("BurnRateLong = %v, want 8.1", got.BurnRateLong)
	}
	if got.WindowErrors != 5 {
		t.Errorf("WindowErrors = %d, want 5", got.WindowErrors)
	}
	if got.WindowRequests != 120 {
		t.Errorf("WindowRequests = %d, want 120", got.WindowRequests)
	}
	if !got.ReadAt.Equal(readAt) {
		t.Errorf("ReadAt = %v, want %v", got.ReadAt, readAt)
	}
}

// TestToLatestReadingResponse_AnyFieldNil_ReturnsNil covers the
// all-or-nothing rule: no reading yet (every column nil) or any single
// missing column yields nil, never a half-filled reading.
func TestToLatestReadingResponse_AnyFieldNil_ReturnsNil(t *testing.T) {
	readAt := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	cases := map[string]func(s *db.Service){
		"no reading at all":        func(s *db.Service) { *s = db.Service{ID: "svc-1"} },
		"read_at nil":              func(s *db.Service) { s.LastReadingAt = nil },
		"burn_rate_short nil":      func(s *db.Service) { s.LastBurnRateShort = nil },
		"burn_rate_long nil":       func(s *db.Service) { s.LastBurnRateLong = nil },
		"window_errors nil":        func(s *db.Service) { s.LastWindowErrors = nil },
		"window_requests nil":      func(s *db.Service) { s.LastWindowRequests = nil },
		"only last_error_at given": func(s *db.Service) { *s = db.Service{ID: "svc-1", LastErrorAt: timePtr(readAt)} },
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			service := serviceWithReading(1, 1, 1, 10, readAt)
			mutate(service)
			if got := toLatestReadingResponse(service); got != nil {
				t.Errorf("toLatestReadingResponse() = %+v, want nil", got)
			}
		})
	}
}

// TestToLatestReadingResponse_NonFiniteBurnRate_EncodesAsNull covers the
// target-100 edge (burnRate yields +Inf when errors > 0): the non-finite
// field becomes JSON null, the rest of the reading survives, and the whole
// detail response still encodes (encoding/json rejects Inf/NaN outright).
func TestToLatestReadingResponse_NonFiniteBurnRate_EncodesAsNull(t *testing.T) {
	readAt := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	cases := map[string]struct {
		short, long float64
	}{
		"short +Inf":   {short: math.Inf(1), long: 2.5},
		"long +Inf":    {short: 2.5, long: math.Inf(1)},
		"both +Inf":    {short: math.Inf(1), long: math.Inf(1)},
		"short NaN":    {short: math.NaN(), long: 2.5},
		"long -Inf":    {short: 2.5, long: math.Inf(-1)},
		"both finite0": {short: 0, long: 0},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			service := serviceWithReading(tc.short, tc.long, 3, 40, readAt)
			resp := serviceDetailResponse{LatestReading: toLatestReadingResponse(service)}

			body, err := json.Marshal(resp)
			if err != nil {
				t.Fatalf("json.Marshal() returned unexpected error: %v", err)
			}

			var decoded struct {
				LatestReading map[string]any `json:"latest_reading"`
			}
			if err := json.Unmarshal(body, &decoded); err != nil {
				t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
			}
			if decoded.LatestReading == nil {
				t.Fatalf("latest_reading = null, want an object: %s", body)
			}

			assertBurnField(t, decoded.LatestReading, "burn_rate_short", tc.short)
			assertBurnField(t, decoded.LatestReading, "burn_rate_long", tc.long)
			if decoded.LatestReading["window_errors"] != float64(3) {
				t.Errorf("window_errors = %v, want 3", decoded.LatestReading["window_errors"])
			}
			if decoded.LatestReading["window_requests"] != float64(40) {
				t.Errorf("window_requests = %v, want 40", decoded.LatestReading["window_requests"])
			}
		})
	}
}

func assertBurnField(t *testing.T, reading map[string]any, key string, want float64) {
	t.Helper()
	got, present := reading[key]
	if !present {
		t.Errorf("%s missing from latest_reading, want present (null or number)", key)
		return
	}
	if math.IsInf(want, 0) || math.IsNaN(want) {
		if got != nil {
			t.Errorf("%s = %v, want null for non-finite %v", key, got, want)
		}
		return
	}
	if got != want {
		t.Errorf("%s = %v, want %v", key, got, want)
	}
}

// TestServiceDetailResponse_JSONShape pins the exact latest_reading keys
// (the frontend's ServiceDetail type mirrors them) and that last_error_at
// is never part of the contract.
func TestServiceDetailResponse_JSONShape(t *testing.T) {
	readAt := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	service := serviceWithReading(12.3, 8.1, 5, 120, readAt)
	service.LastErrorAt = timePtr(readAt)

	withReading, err := json.Marshal(serviceDetailResponse{LatestReading: toLatestReadingResponse(service)})
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}
	want := `"latest_reading":{"burn_rate_short":12.3,"burn_rate_long":8.1,"window_errors":5,"window_requests":120,"read_at":"2026-10-22T12:00:00Z"}`
	if !strings.Contains(string(withReading), want) {
		t.Errorf("body = %s, want it to contain %s", withReading, want)
	}
	if strings.Contains(string(withReading), "last_error_at") {
		t.Errorf("body leaks last_error_at: %s", withReading)
	}

	withoutReading, err := json.Marshal(serviceDetailResponse{LatestReading: toLatestReadingResponse(&db.Service{})})
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}
	if !strings.Contains(string(withoutReading), `"latest_reading":null`) {
		t.Errorf("body = %s, want latest_reading:null", withoutReading)
	}
}

// TestServiceResponse_ListShape_NoLatestReading guards the lean list
// payload: serviceResponse (used by List/Create/Update) never carries the
// reading, only serviceDetailResponse does.
func TestServiceResponse_ListShape_NoLatestReading(t *testing.T) {
	readAt := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	service := serviceWithReading(12.3, 8.1, 5, 120, readAt)

	body, err := json.Marshal(toServiceResponse(service, nil, nil))
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}
	for _, key := range []string{"latest_reading", "burn_rate", "window_errors", "window_requests"} {
		if strings.Contains(string(body), key) {
			t.Errorf("serviceResponse JSON contains %q, want it only on the detail response: %s", key, body)
		}
	}
}
