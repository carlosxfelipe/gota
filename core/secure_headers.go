package core

import (
	"strconv"
)

// SecureHeadersOptions configures the SecureHeaders middleware.
// Zero values select safe defaults; use the "Disable"/explicit fields to opt out.
type SecureHeadersOptions struct {
	// FrameOptions sets X-Frame-Options (default: "DENY"). Use "-" to omit the header.
	FrameOptions string
	// ReferrerPolicy sets Referrer-Policy (default: "no-referrer"). Use "-" to omit.
	ReferrerPolicy string
	// ContentSecurityPolicy sets Content-Security-Policy. Empty (default) omits the header.
	// For pure JSON APIs, ApiContentSecurityPolicy is a strict choice; do NOT apply it to
	// the built-in /docs page, which needs to load the Scalar script and inline styles.
	ContentSecurityPolicy string
	// PermissionsPolicy sets Permissions-Policy. Empty (default) omits the header.
	PermissionsPolicy string
	// HSTSMaxAge sets the Strict-Transport-Security max-age in seconds (default: 31536000).
	// A negative value disables HSTS. The header is only sent for HTTPS requests (direct TLS,
	// or X-Forwarded-Proto: https when Config.TrustProxyHeaders is enabled), as required by RFC 6797.
	HSTSMaxAge int
	// HSTSIncludeSubdomains adds includeSubDomains to the HSTS header.
	HSTSIncludeSubdomains bool
	// HSTSPreload adds preload to the HSTS header (requires HSTSIncludeSubdomains and a long max-age).
	HSTSPreload bool
}

// ApiContentSecurityPolicy is a strict CSP suited for responses that are only JSON.
const ApiContentSecurityPolicy = "default-src 'none'; frame-ancestors 'none'; base-uri 'none'"

// SecureHeaders returns a middleware that sets common security response headers:
// X-Content-Type-Options: nosniff, X-Frame-Options, Referrer-Policy, optionally
// Content-Security-Policy / Permissions-Policy, and Strict-Transport-Security over HTTPS.
// Headers are set before the handler runs, so they also cover error responses.
func SecureHeaders(opts ...SecureHeadersOptions) MiddlewareFunc {
	cfg := SecureHeadersOptions{}
	if len(opts) > 0 {
		cfg = opts[0]
	}
	if cfg.FrameOptions == "" {
		cfg.FrameOptions = "DENY"
	}
	if cfg.ReferrerPolicy == "" {
		cfg.ReferrerPolicy = "no-referrer"
	}
	if cfg.HSTSMaxAge == 0 {
		cfg.HSTSMaxAge = 31536000
	}

	hsts := ""
	if cfg.HSTSMaxAge > 0 {
		hsts = "max-age=" + strconv.Itoa(cfg.HSTSMaxAge)
		if cfg.HSTSIncludeSubdomains {
			hsts += "; includeSubDomains"
		}
		if cfg.HSTSPreload {
			hsts += "; preload"
		}
	}

	return func(next HandlerFunc) HandlerFunc {
		return func(ctx *Context) error {
			h := ctx.ResponseWriter.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			if cfg.FrameOptions != "-" {
				h.Set("X-Frame-Options", cfg.FrameOptions)
			}
			if cfg.ReferrerPolicy != "-" {
				h.Set("Referrer-Policy", cfg.ReferrerPolicy)
			}
			if cfg.ContentSecurityPolicy != "" {
				h.Set("Content-Security-Policy", cfg.ContentSecurityPolicy)
			}
			if cfg.PermissionsPolicy != "" {
				h.Set("Permissions-Policy", cfg.PermissionsPolicy)
			}
			if hsts != "" && isHTTPSRequest(ctx) {
				h.Set("Strict-Transport-Security", hsts)
			}
			return next(ctx)
		}
	}
}

func isHTTPSRequest(ctx *Context) bool {
	return isRequestTLS(ctx)
}
