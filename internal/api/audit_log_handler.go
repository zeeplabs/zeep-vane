package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// auditLogDefaultLimit/MaxLimit bound GET /api/audit-log's limit query
// param (recent-team-activity spec.md AC6/edge cases): missing, zero,
// negative, or non-numeric falls back to the default; anything above the
// max is silently capped, mirroring this codebase's existing "invalid/
// out-of-range input silently falls back to a safe default" posture for
// pagination (parsePage).
const auditLogDefaultLimit = 5
const auditLogMaxLimit = 20

// auditLogLister is the subset of *db.AuditLogRepository the handler
// depends on.
type auditLogLister interface {
	ListRecent(ctx context.Context, limit int) ([]db.AuditLogEntry, error)
}

// AuditLogHandler serves GET /api/audit-log for the Overview page's
// "Atividade recente do time" card. Deliberately not wrapped in the
// generic Page[T] envelope (AD-012) - this is a fixed-size (<=20),
// non-paginated summary feed, not a browsable list screen (design.md).
type AuditLogHandler struct {
	auditLog auditLogLister
	logger   *zap.Logger
}

// NewAuditLogHandler builds an AuditLogHandler backed by auditLog.
func NewAuditLogHandler(auditLog auditLogLister, logger *zap.Logger) *AuditLogHandler {
	return &AuditLogHandler{auditLog: auditLog, logger: logger}
}

// auditLogEntryResponse is the wire shape of one audit_log entry. Locale
// decisions (e.g. substituting a "Removed user" placeholder for a deleted
// actor) stay in the frontend, which already owns every other i18n
// string on this page - the server only reports the stable ActorDeleted
// flag plus whatever name it has (design.md: AuditLogHandler).
type auditLogEntryResponse struct {
	Action       string    `json:"action"`
	TargetLabel  *string   `json:"target_label"`
	ActorName    string    `json:"actor_name"`
	ActorDeleted bool      `json:"actor_deleted"`
	CreatedAt    time.Time `json:"created_at"`
}

func toAuditLogEntryResponse(entry db.AuditLogEntry) auditLogEntryResponse {
	return auditLogEntryResponse{
		Action:       entry.Action,
		TargetLabel:  entry.TargetLabel,
		ActorName:    entry.ActorName,
		ActorDeleted: entry.ActorDeleted,
		CreatedAt:    entry.CreatedAt,
	}
}

// parseAuditLogLimit reads the "limit" query param per spec.md's edge
// cases: missing/invalid/<=0 falls back to auditLogDefaultLimit; a value
// above auditLogMaxLimit is capped to it.
func parseAuditLogLimit(r *http.Request) int {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return auditLogDefaultLimit
	}

	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return auditLogDefaultLimit
	}
	if limit > auditLogMaxLimit {
		return auditLogMaxLimit
	}

	return limit
}

// Get handles GET /api/audit-log?limit=N - any authenticated role, no
// role gate (spec.md: Authorization). Tenant isolation is enforced by
// AuditLogRepository's underlying RLS policy, not by this handler.
func (h *AuditLogHandler) Get(w http.ResponseWriter, r *http.Request) {
	limit := parseAuditLogLimit(r)

	entries, err := h.auditLog.ListRecent(r.Context(), limit)
	if err != nil {
		h.logger.Error("audit-log: failed to list recent entries", zap.Error(err))
		writeInternalError(w)
		return
	}

	resp := make([]auditLogEntryResponse, len(entries))
	for i, entry := range entries {
		resp[i] = toAuditLogEntryResponse(entry)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
