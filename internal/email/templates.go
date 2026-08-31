package email

import (
	"bytes"
	"embed"
	"fmt"
	htmltemplate "html/template"
	texttemplate "text/template"
)

//go:embed templates/admin_invite.html.tmpl templates/admin_invite.txt.tmpl templates/password_reset.html.tmpl templates/password_reset.txt.tmpl
var templateFS embed.FS

const (
	adminInviteHTMLTemplatePath   = "templates/admin_invite.html.tmpl"
	adminInviteTextTemplatePath   = "templates/admin_invite.txt.tmpl"
	passwordResetHTMLTemplatePath = "templates/password_reset.html.tmpl"
	passwordResetTextTemplatePath = "templates/password_reset.txt.tmpl"
)

// templates holds every parsed template this package renders. Parsed once
// in NewService (fail-fast at boot, not at first send) rather than
// panicking via template.Must - a malformed embedded template should
// surface as a clear startup error, not crash the process.
type templates struct {
	adminInviteHTML   *htmltemplate.Template
	adminInviteText   *texttemplate.Template
	passwordResetHTML *htmltemplate.Template
	passwordResetText *texttemplate.Template
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

	return &templates{
		adminInviteHTML:   adminInviteHTML,
		adminInviteText:   adminInviteText,
		passwordResetHTML: passwordResetHTML,
		passwordResetText: passwordResetText,
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
