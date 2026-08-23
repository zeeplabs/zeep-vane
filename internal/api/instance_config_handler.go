package api

import (
	"encoding/json"
	"net/http"

	"go.uber.org/zap"
)

// InstanceConfigHandler exposes operator-configured, install-wide
// settings that have no natural home on an existing resource. It has no
// DB dependency at all - dnsTarget is injected once at construction from
// config.Config.PublicDNSTarget.
type InstanceConfigHandler struct {
	dnsTarget string
	logger    *zap.Logger
}

// NewInstanceConfigHandler builds an InstanceConfigHandler that always
// reports dnsTarget.
func NewInstanceConfigHandler(dnsTarget string, logger *zap.Logger) *InstanceConfigHandler {
	return &InstanceConfigHandler{dnsTarget: dnsTarget, logger: logger}
}

type dnsTargetResponse struct {
	Target *string `json:"target"`
}

// DNSTarget handles GET /api/instance/dns-target, surfacing the DNS
// record value (SPD-10) the attach-domain screen shows the admin, or null
// when the operator never configured PUBLIC_DNS_TARGET.
func (h *InstanceConfigHandler) DNSTarget(w http.ResponseWriter, r *http.Request) {
	var target *string
	if h.dnsTarget != "" {
		target = &h.dnsTarget
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(dnsTargetResponse{Target: target})
}
