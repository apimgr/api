package config

// ContactConfig is the unified "where do messages go" tree from AI.md
// PART 12. Every role carries an email address plus any number of webhook
// transports; Admin is the universal fallback for the other three.
type ContactConfig struct {
	// Admin receives server-internal alerts and is never public.
	Admin ContactRoleConfig `yaml:"admin"`
	// Security receives vulnerability reports and is surfaced publicly in
	// security.txt. Defaults to security@{fqdn} per RFC 2142.
	Security ContactRoleConfig `yaml:"security"`
	// Abuse receives abuse reports. Defaults empty — the server never
	// auto-advertises an unprovisioned abuse@ mailbox.
	Abuse ContactRoleConfig `yaml:"abuse"`
	// General receives /server/contact form submissions.
	General ContactRoleConfig `yaml:"general"`
}

// ContactRoleConfig is one role's delivery configuration.
type ContactRoleConfig struct {
	Email    string                `yaml:"email"`
	Webhooks ContactWebhooksConfig `yaml:"webhooks"`
}

// ContactWebhooksConfig holds the built-in webhook transports. Generic
// receives the raw JSON payload for anything without a dedicated adapter.
type ContactWebhooksConfig struct {
	Telegram string `yaml:"telegram"`
	Discord  string `yaml:"discord"`
	Slack    string `yaml:"slack"`
	Generic  string `yaml:"generic"`
}

// IsEmpty reports whether the role has neither an email address nor any
// webhook transport configured, meaning dispatch must fall back.
func (r ContactRoleConfig) IsEmpty() bool {
	return r.Email == "" && r.Webhooks == ContactWebhooksConfig{}
}

// defaultContactConfig returns the PART 12 defaults for the given FQDN.
func defaultContactConfig(fqdn string) ContactConfig {
	return ContactConfig{
		Admin:    ContactRoleConfig{Email: "admin@" + fqdn},
		Security: ContactRoleConfig{Email: "security@" + fqdn},
		Abuse:    ContactRoleConfig{},
		General:  ContactRoleConfig{},
	}
}

// ResolveAdmin returns the admin role, which every other role falls back to.
func (c ContactConfig) ResolveAdmin() ContactRoleConfig {
	return c.Admin
}

// ResolveSecurity applies the PART 12 security fallback: the configured
// security role, or admin when it was explicitly blanked.
func (c ContactConfig) ResolveSecurity() ContactRoleConfig {
	if c.Security.IsEmpty() {
		return c.Admin
	}
	return c.Security
}

// ResolveGeneral applies the PART 12 general fallback chain: general, then
// admin.
func (c ContactConfig) ResolveGeneral() ContactRoleConfig {
	if c.General.IsEmpty() {
		return c.Admin
	}
	return c.General
}

// ResolveAbuse applies the PART 12 abuse fallback chain: abuse, then
// general, then admin.
func (c ContactConfig) ResolveAbuse() ContactRoleConfig {
	if !c.Abuse.IsEmpty() {
		return c.Abuse
	}
	return c.ResolveGeneral()
}
