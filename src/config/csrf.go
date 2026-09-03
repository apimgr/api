package config

// CSRFConfig holds the server.csrf tree from AI.md PART 16 "CSRF
// Protection". The pattern is stateless double-submit: the token lives in a
// non-HttpOnly SameSite=Strict cookie and must be echoed back in a header or
// form field on every mutating browser request.
type CSRFConfig struct {
	// Enabled turns the check off entirely. Only appropriate for API-only
	// deployments that serve no browser forms.
	Enabled bool `yaml:"enabled"`
	// TokenLength is the token entropy in bytes.
	TokenLength int `yaml:"token_length"`
	// CookieName is the double-submit cookie name.
	CookieName string `yaml:"cookie_name"`
	// HeaderName is the request header carrying the echoed token. The same
	// name is used for the hidden form field.
	HeaderName string `yaml:"header_name"`
	// Secure is "auto", "true" or "false"; "auto" sets the Secure attribute
	// whenever the request proto is https.
	Secure string `yaml:"secure"`
	// ExemptPaths are operator-declared endpoints that skip validation,
	// typically webhook receivers and external callbacks. Glob patterns are
	// supported.
	ExemptPaths []string `yaml:"exempt_paths"`
}

// defaultCSRFConfig returns the PART 16 defaults: enabled, 32 byte tokens,
// csrf_token / X-CSRF-Token naming, Secure resolved from the request proto,
// and no exempt paths.
func defaultCSRFConfig() CSRFConfig {
	return CSRFConfig{
		Enabled:     true,
		TokenLength: 32,
		CookieName:  "csrf_token",
		HeaderName:  "X-CSRF-Token",
		Secure:      "auto",
		ExemptPaths: []string{},
	}
}

// Normalized returns the config with any missing or invalid value replaced
// by its default, so an operator typo degrades to the safe setting rather
// than disabling protection.
func (c CSRFConfig) Normalized() CSRFConfig {
	def := defaultCSRFConfig()
	if c.TokenLength <= 0 {
		c.TokenLength = def.TokenLength
	}
	if c.CookieName == "" {
		c.CookieName = def.CookieName
	}
	if c.HeaderName == "" {
		c.HeaderName = def.HeaderName
	}
	switch c.Secure {
	case "auto", "true", "false":
	default:
		c.Secure = def.Secure
	}
	return c
}
