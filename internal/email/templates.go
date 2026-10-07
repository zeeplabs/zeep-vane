package email

import (
	"bytes"
	"embed"
	"fmt"
	htmltemplate "html/template"
	"path"
	texttemplate "text/template"
)

//go:embed templates/layout.html.tmpl templates/layout.txt.tmpl templates/admin_invite.html.tmpl templates/admin_invite.txt.tmpl templates/password_reset.html.tmpl templates/password_reset.txt.tmpl templates/signup_verification.html.tmpl templates/signup_verification.txt.tmpl templates/incident_opened.html.tmpl templates/incident_opened.txt.tmpl templates/incident_resolved.html.tmpl templates/incident_resolved.txt.tmpl templates/weekly_digest.html.tmpl templates/weekly_digest.txt.tmpl templates/domain_expiring.html.tmpl templates/domain_expiring.txt.tmpl templates/domain_ns_drift.html.tmpl templates/domain_ns_drift.txt.tmpl
var templateFS embed.FS

const (
	layoutHTMLTemplatePath             = "templates/layout.html.tmpl"
	layoutTextTemplatePath             = "templates/layout.txt.tmpl"
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
	domainExpiringHTMLTemplatePath     = "templates/domain_expiring.html.tmpl"
	domainExpiringTextTemplatePath     = "templates/domain_expiring.txt.tmpl"
	domainNSDriftHTMLTemplatePath      = "templates/domain_ns_drift.html.tmpl"
	domainNSDriftTextTemplatePath      = "templates/domain_ns_drift.txt.tmpl"
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
	domainExpiringHTML     *htmltemplate.Template
	domainExpiringText     *texttemplate.Template
	domainNSDriftHTML      *htmltemplate.Template
	domainNSDriftText      *texttemplate.Template
}

// msoOfficeSettings is the Outlook (MSO) conditional-comment block from the
// email handoff's <head>. html/template strips HTML comments from template
// text, so it is emitted through this func instead. It is a fixed literal
// carrying no user data, so returning template.HTML here does not bypass
// escaping of any user-provided value.
const msoOfficeSettings = `<!--[if mso]>
<noscript><xml><o:OfficeDocumentSettings><o:PixelsPerInch>96</o:PixelsPerInch></o:OfficeDocumentSettings></xml></noscript>
<![endif]-->`

var htmlLayoutFuncs = htmltemplate.FuncMap{
	"msoOfficeSettings": func() htmltemplate.HTML { return htmltemplate.HTML(msoOfficeSettings) },
}

// parseLayoutHTML parses one of the six redesigned email HTML templates
// together with the shared layout partials (head, brand header, footer).
// The returned template is named after mainPath's base name, so Execute
// runs the email itself rather than one of the partials.
func parseLayoutHTML(mainPath string) (*htmltemplate.Template, error) {
	return htmltemplate.New(path.Base(mainPath)).Funcs(htmlLayoutFuncs).ParseFS(templateFS, mainPath, layoutHTMLTemplatePath)
}

// parseLayoutText is parseLayoutHTML's plain-text counterpart.
func parseLayoutText(mainPath string) (*texttemplate.Template, error) {
	return texttemplate.New(path.Base(mainPath)).ParseFS(templateFS, mainPath, layoutTextTemplatePath)
}

// parseTemplates parses every embedded email template, returning an error
// (never panicking) if any fails to parse.
func parseTemplates() (*templates, error) {
	adminInviteHTML, err := parseLayoutHTML(adminInviteHTMLTemplatePath)
	if err != nil {
		return nil, fmt.Errorf("email: failed to parse admin invite html template: %w", err)
	}

	adminInviteText, err := parseLayoutText(adminInviteTextTemplatePath)
	if err != nil {
		return nil, fmt.Errorf("email: failed to parse admin invite text template: %w", err)
	}

	passwordResetHTML, err := parseLayoutHTML(passwordResetHTMLTemplatePath)
	if err != nil {
		return nil, fmt.Errorf("email: failed to parse password reset html template: %w", err)
	}

	passwordResetText, err := parseLayoutText(passwordResetTextTemplatePath)
	if err != nil {
		return nil, fmt.Errorf("email: failed to parse password reset text template: %w", err)
	}

	signupVerificationHTML, err := parseLayoutHTML(signupVerificationHTMLTemplatePath)
	if err != nil {
		return nil, fmt.Errorf("email: failed to parse signup verification html template: %w", err)
	}

	signupVerificationText, err := parseLayoutText(signupVerificationTextTemplatePath)
	if err != nil {
		return nil, fmt.Errorf("email: failed to parse signup verification text template: %w", err)
	}

	incidentOpenedHTML, err := parseLayoutHTML(incidentOpenedHTMLTemplatePath)
	if err != nil {
		return nil, fmt.Errorf("email: failed to parse incident opened html template: %w", err)
	}

	incidentOpenedText, err := parseLayoutText(incidentOpenedTextTemplatePath)
	if err != nil {
		return nil, fmt.Errorf("email: failed to parse incident opened text template: %w", err)
	}

	incidentResolvedHTML, err := parseLayoutHTML(incidentResolvedHTMLTemplatePath)
	if err != nil {
		return nil, fmt.Errorf("email: failed to parse incident resolved html template: %w", err)
	}

	incidentResolvedText, err := parseLayoutText(incidentResolvedTextTemplatePath)
	if err != nil {
		return nil, fmt.Errorf("email: failed to parse incident resolved text template: %w", err)
	}

	weeklyDigestHTML, err := parseLayoutHTML(weeklyDigestHTMLTemplatePath)
	if err != nil {
		return nil, fmt.Errorf("email: failed to parse weekly digest html template: %w", err)
	}

	weeklyDigestText, err := parseLayoutText(weeklyDigestTextTemplatePath)
	if err != nil {
		return nil, fmt.Errorf("email: failed to parse weekly digest text template: %w", err)
	}

	domainExpiringHTML, err := htmltemplate.ParseFS(templateFS, domainExpiringHTMLTemplatePath)
	if err != nil {
		return nil, fmt.Errorf("email: failed to parse domain expiring html template: %w", err)
	}

	domainExpiringText, err := texttemplate.ParseFS(templateFS, domainExpiringTextTemplatePath)
	if err != nil {
		return nil, fmt.Errorf("email: failed to parse domain expiring text template: %w", err)
	}

	domainNSDriftHTML, err := htmltemplate.ParseFS(templateFS, domainNSDriftHTMLTemplatePath)
	if err != nil {
		return nil, fmt.Errorf("email: failed to parse domain ns drift html template: %w", err)
	}

	domainNSDriftText, err := texttemplate.ParseFS(templateFS, domainNSDriftTextTemplatePath)
	if err != nil {
		return nil, fmt.Errorf("email: failed to parse domain ns drift text template: %w", err)
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
		domainExpiringHTML:     domainExpiringHTML,
		domainExpiringText:     domainExpiringText,
		domainNSDriftHTML:      domainNSDriftHTML,
		domainNSDriftText:      domainNSDriftText,
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

// renderDomainExpiring renders both the HTML and plain-text domain-expiring
// bodies from data.
func (t *templates) renderDomainExpiring(data DomainExpiringEmailData) (htmlBody, textBody string, err error) {
	var htmlBuf bytes.Buffer
	if err := t.domainExpiringHTML.Execute(&htmlBuf, data); err != nil {
		return "", "", fmt.Errorf("email: failed to render domain expiring html template: %w", err)
	}

	var textBuf bytes.Buffer
	if err := t.domainExpiringText.Execute(&textBuf, data); err != nil {
		return "", "", fmt.Errorf("email: failed to render domain expiring text template: %w", err)
	}

	return htmlBuf.String(), textBuf.String(), nil
}

// renderDomainNSDrift renders both the HTML and plain-text domain NS-drift
// bodies from data.
func (t *templates) renderDomainNSDrift(data DomainNSDriftEmailData) (htmlBody, textBody string, err error) {
	var htmlBuf bytes.Buffer
	if err := t.domainNSDriftHTML.Execute(&htmlBuf, data); err != nil {
		return "", "", fmt.Errorf("email: failed to render domain ns drift html template: %w", err)
	}

	var textBuf bytes.Buffer
	if err := t.domainNSDriftText.Execute(&textBuf, data); err != nil {
		return "", "", fmt.Errorf("email: failed to render domain ns drift text template: %w", err)
	}

	return htmlBuf.String(), textBuf.String(), nil
}
