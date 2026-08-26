package api

import "net/http"

// defaultCSP is a same-origin-only policy: the SPA loads its own JS/CSS
// bundles and nothing else (web/dist/index.html has no third-party script
// or font references), and every API call it makes is same-origin. Inline
// styles are allowed (some components set the style prop directly) - inline
// script is not.
const defaultCSP = "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; base-uri 'self'; frame-ancestors 'none'"

// SecurityHeaders sets baseline security headers on every response (M14) -
// previously nothing but CORS touched response headers on either listener.
// hsts should be true only on a listener that is itself terminating real
// TLS (the public status-page HTTPS listener) - sending
// Strict-Transport-Security over the admin HTTP listener would be a false
// promise for anyone reaching this instance over plain HTTP (a browser
// ignores the header outside HTTPS anyway, but it has no business being
// sent there).
func SecurityHeaders(hsts bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Content-Security-Policy", defaultCSP)
			if hsts {
				h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}
