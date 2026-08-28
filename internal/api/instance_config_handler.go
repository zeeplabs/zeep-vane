package api

import (
	"encoding/json"
	"net/http"

	"go.uber.org/zap"
)

// InstanceConfigHandler exposes operator-configured, install-wide
// settings that have no natural home on an existing resource. dnsTarget is
// injected once at construction from config.Config.PublicDNSTarget;
// companySettings backs Branding.
type InstanceConfigHandler struct {
	dnsTarget       string
	companySettings companySettingsGetter
	logger          *zap.Logger
}

// NewInstanceConfigHandler builds an InstanceConfigHandler that always
// reports dnsTarget and companySettings-backed branding.
func NewInstanceConfigHandler(dnsTarget string, companySettings companySettingsGetter, logger *zap.Logger) *InstanceConfigHandler {
	return &InstanceConfigHandler{dnsTarget: dnsTarget, companySettings: companySettings, logger: logger}
}

type dnsTargetResponse struct {
	Target *string `json:"target"`
}

type brandingResponse struct {
	LogoURL *string `json:"logo_url"`
}

// Branding handles GET /api/instance/branding: the company logo shown on
// the login screen and the admin sidebar, both of which render before (or
// regardless of) any role check - unlike GET /api/company-settings
// (owner-only), this is deliberately public/unauthenticated. It leaks no
// more than what every public status page already shows to any visitor
// without auth (SET-15) - only the logo, never contact_email or any other
// company_settings field.
func (h *InstanceConfigHandler) Branding(w http.ResponseWriter, r *http.Request) {
	companySettings, err := h.companySettings.Get(r.Context())
	if err != nil {
		h.logger.Error("instance-config: failed to get company settings for branding", zap.Error(err))
		writeInternalError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(brandingResponse{LogoURL: companySettings.LogoServedURL()})
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
