package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// newRequireRoleRequest builds a request whose context already carries the
// caller's role in the active tenant (as TenantContext would have set it
// upstream), so RequireRole can be tested in isolation, without a real
// database.
func newRequireRoleRequest(role string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	return req.WithContext(WithRole(req.Context(), role))
}

func newRequireRoleHandler(handlerCalled *bool, roles ...string) http.Handler {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})
	return RequireRole(roles...)(next)
}

func TestRequireRole_OwnerAllowed_PassesThrough(t *testing.T) {
	var called bool
	handler := newRequireRoleHandler(&called, db.RoleOwner, db.RoleOperator)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, newRequireRoleRequest(db.RoleOwner))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !called {
		t.Error("handler was not called, want it to run for an allowed role")
	}
}

func TestRequireRole_OwnerDisallowed_403(t *testing.T) {
	var called bool
	handler := newRequireRoleHandler(&called, db.RoleViewer)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, newRequireRoleRequest(db.RoleOwner))

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if called {
		t.Error("handler was called, want it rejected before reaching the handler")
	}
}

func TestRequireRole_OperatorAllowed_PassesThrough(t *testing.T) {
	var called bool
	handler := newRequireRoleHandler(&called, db.RoleOwner, db.RoleOperator)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, newRequireRoleRequest(db.RoleOperator))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !called {
		t.Error("handler was not called, want it to run for an allowed role")
	}
}

func TestRequireRole_OperatorDisallowed_403(t *testing.T) {
	var called bool
	handler := newRequireRoleHandler(&called, db.RoleOwner)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, newRequireRoleRequest(db.RoleOperator))

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if called {
		t.Error("handler was called, want it rejected before reaching the handler")
	}
}

func TestRequireRole_ViewerAllowed_PassesThrough(t *testing.T) {
	var called bool
	handler := newRequireRoleHandler(&called, db.RoleOwner, db.RoleOperator, db.RoleViewer)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, newRequireRoleRequest(db.RoleViewer))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !called {
		t.Error("handler was not called, want it to run for an allowed role")
	}
}

func TestRequireRole_ViewerDisallowed_403(t *testing.T) {
	var called bool
	handler := newRequireRoleHandler(&called, db.RoleOwner, db.RoleOperator)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, newRequireRoleRequest(db.RoleViewer))

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if called {
		t.Error("handler was called, want it rejected before reaching the handler")
	}
}

// TestRequireRole_NoRoleInContext_403 covers both "TenantContext never
// ran" and "the session has no active tenant, so the caller has no role
// anywhere" - the same fail-closed answer for both.
func TestRequireRole_NoRoleInContext_403(t *testing.T) {
	var called bool
	handler := newRequireRoleHandler(&called, db.RoleOwner, db.RoleOperator, db.RoleViewer)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if called {
		t.Error("handler was called, want it rejected when no role was resolved")
	}
}
