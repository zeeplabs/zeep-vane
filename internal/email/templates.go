package email

import (
	"bytes"
	"embed"
	"fmt"
	htmltemplate "html/template"
	texttemplate "text/template"
)

//go:embed templates/admin_invite.html.tmpl templates/admin_invite.txt.tmpl templates/password_reset.html.tmpl templates/password_reset.txt.tmpl templates/signup_verification.html.tmpl templates/signup_verification.txt.tmpl templates/incident_opened.html.tmpl templates/incident_opened.txt.tmpl templates/incident_resolved.html.tmpl templates/incident_resolved.txt.tmpl templates/weekly_digest.html.tmpl templates/weekly_digest.txt.tmpl
var templateFS embed.FS

const (
	adminInviteHTMLTemplatePath        = "templates/admin_invite.html.tmpl"
	adminInviteTextTemplatePath        = "templates/admin_invite.txt.tmpl"
	passwordResetHTMLTemplatePath      = "templates/password_reset.html.tmpl"
	passwordResetTextTemplatePath      = "templates/password_reset.txt.tmpl"
	signupVerificationHTMLTemplatePath = "templates/signup_verification.html.tmpl"
	signupVerificationTextTemplatePath = "templates/signup_verification.txt.tmpl"
	incidentOpenedHTMLTemplatePath     = "templates/incident_opened.html.tmpl"
	incidentOpenedTextTemplatePath     = "templates/incident_opened.txt.tmpl"
	incidentResolvedHTMLTemplatePath   = "templates/incident_resolved.html.tmpl"
	incidentResolvedTextTemplatePath   = "templates/incident_resolved.txt.tmpl"
	weeklyDigestHTMLTemplatePath       = "templates/weekly_digest.html.tmpl"
	weeklyDigestTextTemplatePath       = "templates/weekly_digest.txt.tmpl"
)

// templates holds every parsed template this package renders. Parsed once
// in NewService (fail-fast at boot, not at first send) rather than
// panicking via template.Must - a malformed embedded template should
// surface as a clear startup error, not crash the process.
type templates struct {
	adminInviteHTML        *htmltemplate.Template
	adminInviteText        *texttemplate.Template
	passwordResetHTML      *htmltemplate.Template
	passwordResetText      *texttemplate.Template
	signupVerificationHTML *htmltemplate.Template
	signupVerificationText *texttemplate.Template
	incidentOpenedHTML     *htmltemplate.Template
	incidentOpenedText     *texttemplate.Template
	incidentResolvedHTML   *htmltemplate.Template
	incidentResolvedText   *texttemplate.Template
	weeklyDigestHTML       *htmltemplate.Template
	weeklyDigestText       *texttemplate.Template
}

// parseTemplates parses every embedded email template, returning an error
// (never panicking) if any fails to parse.
func parseTemplates() (*templates, error) {
	adminInviteHTML, err := htmltemplate.ParseFS(templateFS, adminInviteHTMLTemplatePath)
	if err != nil {
		return nil, fmt.Errorf("email: failed to parse admin invite html template: %w", err)
	}

	adminInviteText, err := texttemplate.ParseFS(templateFS, adminInviteTextTemplatePath)
	if err != nil {
		return nil, fmt.Errorf("email: failed to parse admin invite text template: %w", err)
	}

	passwordResetHTML, err := htmltemplate.ParseFS(templateFS, passwordResetHTMLTemplatePath)
	if err != nil {
		return nil, fmt.Errorf("email: failed to parse password reset html template: %w", err)
	}

	passwordResetText, err := texttemplate.ParseFS(templateFS, passwordResetTextTemplatePath)
	if err != nil {
		return nil, fmt.Errorf("email: failed to parse password reset text template: %w", err)
	}

	signupVerificationHTML, err := htmltemplate.ParseFS(templateFS, signupVerificationHTMLTemplatePath)
	if err != nil {
		return nil, fmt.Errorf("email: failed to parse signup verification html template: %w", err)
	}

	signupVerificationText, err := texttemplate.ParseFS(templateFS, signupVerificationTextTemplatePath)
	if err != nil {
		return nil, fmt.Errorf("email: failed to parse signup verification text template: %w", err)
	}

	incidentOpenedHTML, err := htmltemplate.ParseFS(templateFS, incidentOpenedHTMLTemplatePath)
	if err != nil {
		return nil, fmt.Errorf("email: failed to parse incident opened html template: %w", err)
	}

	incidentOpenedText, err := texttemplate.ParseFS(templateFS, incidentOpenedTextTemplatePath)
	if err != nil {
		return nil, fmt.Errorf("email: failed to parse incident opened text template: %w", err)
	}

	incidentResolvedHTML, err := htmltemplate.ParseFS(templateFS, incidentResolvedHTMLTemplatePath)
	if err != nil {
		return nil, fmt.Errorf("email: failed to parse incident resolved html template: %w", err)
	}

	incidentResolvedText, err := texttemplate.ParseFS(templateFS, incidentResolvedTextTemplatePath)
	if err != nil {
		return nil, fmt.Errorf("email: failed to parse incident resolved text template: %w", err)
	}

	weeklyDigestHTML, err := htmltemplate.ParseFS(templateFS, weeklyDigestHTMLTemplatePath)
	if err != nil {
		return nil, fmt.Errorf("email: failed to parse weekly digest html template: %w", err)
	}

	weeklyDigestText, err := texttemplate.ParseFS(templateFS, weeklyDigestTextTemplatePath)
	if err != nil {
		return nil, fmt.Errorf("email: failed to parse weekly digest text template: %w", err)
	}

	return &templates{
		adminInviteHTML:        adminInviteHTML,
		adminInviteText:        adminInviteText,
		passwordResetHTML:      passwordResetHTML,
		passwordResetText:      passwordResetText,
		signupVerificationHTML: signupVerificationHTML,
		signupVerificationText: signupVerificationText,
		incidentOpenedHTML:     incidentOpenedHTML,
		incidentOpenedText:     incidentOpenedText,
		incidentResolvedHTML:   incidentResolvedHTML,
		incidentResolvedText:   incidentResolvedText,
		weeklyDigestHTML:       weeklyDigestHTML,
		weeklyDigestText:       weeklyDigestText,
	}, nil
}

// renderAdminInvite renders both the HTML and plain-text admin-invite
// bodies from data.
func (t *templates) renderAdminInvite(data AdminInviteEmailData) (htmlBody, textBody string, err error) {
	var htmlBuf bytes.Buffer
	if err := t.adminInviteHTML.Execute(&htmlBuf, data); err != nil {
		return "", "", fmt.Errorf("email: failed to render admin invite html template: %w", err)
	}

	var textBuf bytes.Buffer
	if err := t.adminInviteText.Execute(&textBuf, data); err != nil {
		return "", "", fmt.Errorf("email: failed to render admin invite text template: %w", err)
	}

	return htmlBuf.String(), textBuf.String(), nil
}

// renderPasswordReset renders both the HTML and plain-text password-reset
// bodies from data.
func (t *templates) renderPasswordReset(data PasswordResetEmailData) (htmlBody, textBody string, err error) {
	var htmlBuf bytes.Buffer
	if err := t.passwordResetHTML.Execute(&htmlBuf, data); err != nil {
		return "", "", fmt.Errorf("email: failed to render password reset html template: %w", err)
	}

	var textBuf bytes.Buffer
	if err := t.passwordResetText.Execute(&textBuf, data); err != nil {
		return "", "", fmt.Errorf("email: failed to render password reset text template: %w", err)
	}

	return htmlBuf.String(), textBuf.String(), nil
}

// renderSignupVerification renders both the HTML and plain-text signup
// email-verification bodies from data.
func (t *templates) renderSignupVerification(data SignupVerificationEmailData) (htmlBody, textBody string, err error) {
	var htmlBuf bytes.Buffer
	if err := t.signupVerificationHTML.Execute(&htmlBuf, data); err != nil {
		return "", "", fmt.Errorf("email: failed to render signup verification html template: %w", err)
	}

	var textBuf bytes.Buffer
	if err := t.signupVerificationText.Execute(&textBuf, data); err != nil {
		return "", "", fmt.Errorf("email: failed to render signup verification text template: %w", err)
	}

	return htmlBuf.String(), textBuf.String(), nil
}

// renderIncidentOpened renders both the HTML and plain-text incident-opened
// bodies from data.
func (t *templates) renderIncidentOpened(data IncidentOpenedEmailData) (htmlBody, textBody string, err error) {
	var htmlBuf bytes.Buffer
	if err := t.incidentOpenedHTML.Execute(&htmlBuf, data); err != nil {
		return "", "", fmt.Errorf("email: failed to render incident opened html template: %w", err)
	}

	var textBuf bytes.Buffer
	if err := t.incidentOpenedText.Execute(&textBuf, data); err != nil {
		return "", "", fmt.Errorf("email: failed to render incident opened text template: %w", err)
	}

	return htmlBuf.String(), textBuf.String(), nil
}

// renderIncidentResolved renders both the HTML and plain-text incident-resolved
// bodies from data.
func (t *templates) renderIncidentResolved(data IncidentResolvedEmailData) (htmlBody, textBody string, err error) {
	var htmlBuf bytes.Buffer
	if err := t.incidentResolvedHTML.Execute(&htmlBuf, data); err != nil {
		return "", "", fmt.Errorf("email: failed to render incident resolved html template: %w", err)
	}

	var textBuf bytes.Buffer
	if err := t.incidentResolvedText.Execute(&textBuf, data); err != nil {
		return "", "", fmt.Errorf("email: failed to render incident resolved text template: %w", err)
	}

	return htmlBuf.String(), textBuf.String(), nil
}

// renderWeeklyDigest renders both the HTML and plain-text weekly-digest bodies
// from data.
func (t *templates) renderWeeklyDigest(data WeeklyDigestEmailData) (htmlBody, textBody string, err error) {
	var htmlBuf bytes.Buffer
	if err := t.weeklyDigestHTML.Execute(&htmlBuf, data); err != nil {
		return "", "", fmt.Errorf("email: failed to render weekly digest html template: %w", err)
	}

	var textBuf bytes.Buffer
	if err := t.weeklyDigestText.Execute(&textBuf, data); err != nil {
		return "", "", fmt.Errorf("email: failed to render weekly digest text template: %w", err)
	}

	return htmlBuf.String(), textBuf.String(), nil
}
