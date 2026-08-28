package email

import (
	"bytes"
	"embed"
	"fmt"
	htmltemplate "html/template"
	texttemplate "text/template"
)

//go:embed templates/admin_invite.html.tmpl templates/admin_invite.txt.tmpl
var templateFS embed.FS

const (
	adminInviteHTMLTemplatePath = "templates/admin_invite.html.tmpl"
	adminInviteTextTemplatePath = "templates/admin_invite.txt.tmpl"
)

// templates holds every parsed template this package renders. Parsed once
// in NewService (fail-fast at boot, not at first send) rather than
// panicking via template.Must - a malformed embedded template should
// surface as a clear startup error, not crash the process.
type templates struct {
	adminInviteHTML *htmltemplate.Template
	adminInviteText *texttemplate.Template
}

// parseTemplates parses the embedded admin-invite templates, returning an
// error (never panicking) if either fails to parse.
func parseTemplates() (*templates, error) {
	htmlTmpl, err := htmltemplate.ParseFS(templateFS, adminInviteHTMLTemplatePath)
	if err != nil {
		return nil, fmt.Errorf("email: failed to parse admin invite html template: %w", err)
	}

	textTmpl, err := texttemplate.ParseFS(templateFS, adminInviteTextTemplatePath)
	if err != nil {
		return nil, fmt.Errorf("email: failed to parse admin invite text template: %w", err)
	}

	return &templates{adminInviteHTML: htmlTmpl, adminInviteText: textTmpl}, nil
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
