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
	// DPO contact details are public; the name is optional and the email falls
	// back to General on the privacy pages.
	DPO ContactRoleConfig `yaml:"dpo"`
	// Abuse receives abuse reports. Defaults empty — the server never
	// auto-advertises an unprovisioned abuse@ mailbox.
	Abuse ContactRoleConfig `yaml:"abuse"`
	// General receives /server/contact form submissions.
	General ContactRoleConfig `yaml:"general"`
}

// ContactRoleConfig is one role's delivery configuration.
type ContactRoleConfig struct {
	Name     string                `yaml:"name"`
	Email    string                `yaml:"email"`
	Webhooks ContactWebhooksConfig `yaml:"webhooks"`
}

// ContactWebhooksConfig is an open transport-name to URL/secret map. A
// transport's signing key is stored in the sibling <name>_secret entry.
type ContactWebhooksConfig map[string]string

// IsEmpty reports whether the role has neither an email address nor any
// webhook transport configured, meaning dispatch must fall back.
func (r ContactRoleConfig) IsEmpty() bool {
	return r.Email == "" && len(r.Webhooks) == 0
}

// defaultContactConfig returns the PART 12 defaults for the given FQDN.
func defaultContactConfig(fqdn string) ContactConfig {
	return ContactConfig{
		Admin:    ContactRoleConfig{Email: "admin@" + fqdn, Webhooks: defaultWebhookConfig()},
		Security: ContactRoleConfig{Email: "security@" + fqdn, Webhooks: defaultWebhookConfig()},
		Abuse:    ContactRoleConfig{Webhooks: defaultWebhookConfig()},
		General:  ContactRoleConfig{Webhooks: defaultWebhookConfig()},
	}
}

func defaultWebhookConfig() ContactWebhooksConfig {
	return ContactWebhooksConfig{
		"telegram": "", "telegram_secret": "",
		"discord": "", "discord_secret": "",
		"slack": "", "slack_secret": "",
		"mattermost": "", "mattermost_secret": "",
		"pushover": "", "pushover_secret": "",
		"gotify": "", "gotify_secret": "",
		"generic": "", "generic_secret": "",
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

// ResolveDPO applies the privacy contact fallback: DPO, then General.
func (c ContactConfig) ResolveDPO() ContactRoleConfig {
	if c.DPO.Email != "" {
		return c.DPO
	}
	if c.General.Email != "" {
		return c.General
	}
	return ContactRoleConfig{}
}
