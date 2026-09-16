package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// SessionsHandler serves the per-device session-management routes
// backing the redesigned Meu Perfil screen's "Sessões ativas" list
// (user-sessions spec). Self-only, anyRole — no admin-views-another-
// user's-sessions in this MVP (mockup has no such screen).
type SessionsHandler struct {
	sessions *db.SessionRepository
	logger   *zap.Logger
}

// NewSessionsHandler builds a SessionsHandler backed by sessions. logger
// is used for handler-level errors (lookups, revokes); the inner
// repository never panics on a transient DB error and surfaces a real
// error to the caller, who logs and returns 500.
func NewSessionsHandler(sessions *db.SessionRepository, logger *zap.Logger) *SessionsHandler {
	return &SessionsHandler{sessions: sessions, logger: logger}
}

// SessionView is one row of GET /api/auth/sessions's response. Matches
// the SessionRepository.Session shape modulo `current` (computed from
// the request's sid claim, not stored on the row) and sql.NullString /
// sql.NullTime projection to omitempty so NULL columns don't show up as
// "null" in the JSON.
//
// `id` is a string (not uuid.UUID) because the entire `users` /
// `sessions` table family uses string IDs in this codebase — no
// `github.com/google/uuid` dependency added; Postgres UUIDs round-trip
// fine through pgx into Go strings. Matches the SessionRepository.Session
// field convention.
type SessionView struct {
	ID         string     `json:"id"`
	UserAgent  *string    `json:"user_agent,omitempty"`
	IP         *string    `json:"ip,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
	Current    bool       `json:"current"`
}

// sessionNotFoundBody is returned byte-for-byte for "id doesn't exist",
// "id belongs to a different user", and "id is a malformed UUID" — the
// three failure modes that anti-enumeration requires to look
// indistinguishable from the caller's POV (user-sessions spec SESS-10).
const sessionNotFoundBody = `{"error":"session not found"}`

// cannotRevokeCurrentSessionBody is returned for the {id} == ctx.sid
// case (user-sessions spec SESS-09): ending your own current session
// through this endpoint would leave the caller's own next request
// rejected with no clear signal why; Logout already exists as the
// correct action for ending your own current session.
const cannotRevokeCurrentSessionBody = `{"error":"cannot revoke the current session - use POST /api/auth/logout instead"}`

// List handles GET /api/auth/sessions. Returns the authenticated user's
// own non-revoked, non-expired sessions (see
// SessionRepository.ListForUser's filters), with exactly one row marked
// `current: true` - the row whose id matches the request's own sid
// claim (placed in context by RequireAuth). That "current" row never
// shows an "Encerrar" button on the redesigned Meu Perfil screen
// (frontend hides it; this endpoint always returns the row so the UI
// can render it with a distinct badge).
func (h *SessionsHandler) List(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeUnauthorized(w)
		return
	}
	sid, ok := SessionIDFromContext(r.Context())
	if !ok {
		writeUnauthorized(w)
		return
	}

	rows, err := h.sessions.ListForUser(r.Context(), user.ID)
	if err != nil {
		h.logger.Error("sessions: failed to list", zap.Error(err))
		writeInternalError(w)
		return
	}

	out := make([]SessionView, len(rows))
	for i, s := range rows {
		out[i] = sessionToView(s, s.ID == sid)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}

// Revoke handles DELETE /api/auth/sessions/{id}. Revokes a single
// sessions-table row belonging to the caller, OTHER than the caller's
// own current session (the "Encerrar" button on the redesigned Meu
// Perfil screen — the UI doesn't show this button on the current row).
//
// Status codes (user-sessions spec SESS-08/09/10):
//
//	200 + revoked_at set: the row existed, belonged to the caller, and
//	                       is now revoked. The revoked token is rejected
//	                       with 401 by RequireAuth on its very next
//	                       authenticated request.
//	404:                  {id} is malformed UUID, doesn't exist, or
//	                       belongs to a different user. Anti-enumeration:
//	                       the response body is byte-for-byte the same
//	                       regardless of which case it was.
//	409:                  {id} is the caller's own current session —
//	                       they should call POST /api/auth/logout
//	                       instead, which both revokes the row AND
//	                       clears the cookie.
func (h *SessionsHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeUnauthorized(w)
		return
	}
	sid, ok := SessionIDFromContext(r.Context())
	if !ok {
		writeUnauthorized(w)
		return
	}

	id := chi.URLParam(r, "id")

	if id == sid {
		writeAdminError(w, http.StatusConflict, cannotRevokeCurrentSessionBody)
		return
	}

	// GetByIDAndUser returns ErrNotFound when the row doesn't exist OR
	// belongs to a different user (anti-enumeration, matching the spec's
	// "404 (not 403) - never reveal whether a given session ID exists").
	// A malformed UUID surfaces from pgx as a Postgres "invalid_text_representation"
	// (SQLSTATE 22P02) error - we treat that the same as ErrNotFound so
	// the caller can't distinguish "typo" from "id doesn't exist".
	if _, err := h.sessions.GetByIDAndUser(r.Context(), id, user.ID); err != nil {
		if errors.Is(err, db.ErrNotFound) || isInvalidUUIDSyntax(err) {
			writeAdminError(w, http.StatusNotFound, sessionNotFoundBody)
			return
		}
		h.logger.Error("sessions: failed to look up row for revoke", zap.Error(err))
		writeInternalError(w)
		return
	}

	if err := h.sessions.Revoke(r.Context(), id); err != nil {
		h.logger.Error("sessions: failed to revoke", zap.Error(err))
		writeInternalError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// sessionToView projects a db.Session to its JSON-friendly view,
// translating sql.NullString / sql.NullTime to omitempty pointers so
// missing values don't render as literal JSON null. `current` is
// computed by the caller (it depends on the request's sid claim).
func sessionToView(s db.Session, current bool) SessionView {
	out := SessionView{
		ID:        s.ID,
		CreatedAt: s.CreatedAt,
		Current:   current,
	}
	if s.UserAgent.Valid {
		ua := s.UserAgent.String
		out.UserAgent = &ua
	}
	if s.IP.Valid {
		ip := s.IP.String
		out.IP = &ip
	}
	if s.LastSeenAt.Valid {
		t := s.LastSeenAt.Time
		out.LastSeenAt = &t
	}
	return out
}

// isInvalidUUIDSyntax returns true if err is a pgconn error with
// SQLSTATE 22P02 (Postgres "invalid_text_representation"), which is
// what pgx surfaces when the WHERE clause compares a UUID column
// against a non-UUID string literal. Treated identically to ErrNotFound
// at the Revoke handler boundary (anti-enumeration - the caller can't
// distinguish "typo in the URL" from "id doesn't exist").
func isInvalidUUIDSyntax(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "22P02"
}
