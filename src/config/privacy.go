package config

// PrivacyConfig holds the server-wide privacy and cookie-consent settings
// from AI.md PART 12. Data.Sold drives the dynamic messaging used by the
// consent banner, the cookie descriptions, and the privacy page.
type PrivacyConfig struct {
	Data       PrivacyDataConfig       `yaml:"data"`
	Retention  PrivacyRetentionConfig  `yaml:"retention"`
	Consent    PrivacyConsentConfig    `yaml:"consent"`
	Cookies    PrivacyCookiesConfig    `yaml:"cookies"`
	ThirdParty PrivacyThirdPartyConfig `yaml:"third_party"`
	Content    PrivacyContentConfig    `yaml:"content"`
}

// PrivacyDataConfig describes how user data is handled.
type PrivacyDataConfig struct {
	// Sold flips every piece of consent/privacy copy to the CCPA
	// "may be sold" wording and enables the Do Not Sell section.
	Sold bool `yaml:"sold"`
	// StoredOnServer states that data lives on this server rather than a
	// third-party cloud.
	StoredOnServer bool `yaml:"stored_on_server"`
	// Sharing enumerates the conditions under which data may reach a third
	// party.
	Sharing []PrivacySharingConfig `yaml:"sharing"`
}

// PrivacySharingConfig is one third-party sharing condition.
type PrivacySharingConfig struct {
	Condition string `yaml:"condition"`
	When      string `yaml:"when"`
	Data      string `yaml:"data"`
}

// PrivacyRetentionConfig describes how long data is kept and which user
// rights are available.
type PrivacyRetentionConfig struct {
	Period            string `yaml:"period"`
	ExportAvailable   bool   `yaml:"export_available"`
	DeletionAvailable bool   `yaml:"deletion_available"`
}

// PrivacyConsentConfig configures the cookie consent banner.
type PrivacyConsentConfig struct {
	ShowUntilAcknowledged bool                       `yaml:"show_until_acknowledged"`
	DefaultEnabled        bool                       `yaml:"default_enabled"`
	Message               string                     `yaml:"message"`
	MessageIfSold         string                     `yaml:"message_if_sold"`
	Policy                PrivacyPolicyLinkConfig    `yaml:"policy"`
	Buttons               PrivacyConsentButtonConfig `yaml:"buttons"`
	Position              string                     `yaml:"position"`
	ShowPreferences       bool                       `yaml:"show_preferences"`
	PreferencesText       string                     `yaml:"preferences_text"`
}

// PrivacyPolicyLinkConfig is the consent banner's privacy policy link.
type PrivacyPolicyLinkConfig struct {
	Text string `yaml:"text"`
	URL  string `yaml:"url"`
}

// PrivacyConsentButtonConfig holds the consent banner button labels.
type PrivacyConsentButtonConfig struct {
	Decline string `yaml:"decline"`
	Accept  string `yaml:"accept"`
}

// PrivacyCookiesConfig describes the three cookie categories. Essential is
// always enabled and cannot be turned off.
type PrivacyCookiesConfig struct {
	Essential   PrivacyCookieCategoryConfig  `yaml:"essential"`
	Preferences PrivacyCookieCategoryConfig  `yaml:"preferences"`
	Analytics   PrivacyAnalyticsCookieConfig `yaml:"analytics"`
}

// PrivacyCookieCategoryConfig is one cookie category's state and copy.
type PrivacyCookieCategoryConfig struct {
	Enabled     bool   `yaml:"enabled"`
	Description string `yaml:"description"`
}

// PrivacyAnalyticsCookieConfig adds the data.sold-dependent suffixes to the
// analytics category description.
type PrivacyAnalyticsCookieConfig struct {
	Enabled                  bool   `yaml:"enabled"`
	Description              string `yaml:"description"`
	DescriptionSuffixNotSold string `yaml:"description_suffix_not_sold"`
	DescriptionSuffixSold    string `yaml:"description_suffix_sold"`
}

// PrivacyThirdPartyConfig lists third-party services, auto-populated from
// the tracking config plus any manual entries.
type PrivacyThirdPartyConfig struct {
	Services []PrivacyThirdPartyService `yaml:"services"`
}

// PrivacyThirdPartyService is one third-party service disclosure.
type PrivacyThirdPartyService struct {
	Name      string `yaml:"name"`
	Purpose   string `yaml:"purpose"`
	DataSent  string `yaml:"data_sent"`
	PolicyURL string `yaml:"policy_url"`
}

// PrivacyContentConfig holds the markdown sections rendered on the privacy
// page. DataUsageIfSold replaces DataUsage when Data.Sold is true.
type PrivacyContentConfig struct {
	DataCollection  string `yaml:"data_collection"`
	DataUsage       string `yaml:"data_usage"`
	DataUsageIfSold string `yaml:"data_usage_if_sold"`
	DataSecurity    string `yaml:"data_security"`
}

// ConsentMessage returns the banner copy matching the current data.sold
// setting.
func (p PrivacyConfig) ConsentMessage() string {
	if p.Data.Sold {
		return p.Consent.MessageIfSold
	}
	return p.Consent.Message
}

// AnalyticsCookieDescription returns the analytics cookie description with
// the sold/not-sold suffix appended.
func (p PrivacyConfig) AnalyticsCookieDescription() string {
	suffix := p.Cookies.Analytics.DescriptionSuffixNotSold
	if p.Data.Sold {
		suffix = p.Cookies.Analytics.DescriptionSuffixSold
	}
	if suffix == "" {
		return p.Cookies.Analytics.Description
	}
	if p.Cookies.Analytics.Description == "" {
		return suffix
	}
	return p.Cookies.Analytics.Description + " " + suffix
}

// DataUsageContent returns the data-usage privacy page section matching the
// current data.sold setting.
func (p PrivacyConfig) DataUsageContent() string {
	if p.Data.Sold {
		return p.Content.DataUsageIfSold
	}
	return p.Content.DataUsage
}

// defaultPrivacyConfig returns the PART 12 defaults verbatim.
func defaultPrivacyConfig() PrivacyConfig {
	return PrivacyConfig{
		Data: PrivacyDataConfig{
			Sold:           false,
			StoredOnServer: true,
			Sharing: []PrivacySharingConfig{
				{
					Condition: "analytics",
					When:      "Tracking configured (server.tracking.type set) AND user consents",
					Data:      "Anonymized: page views, browser type, country",
				},
				{
					Condition: "email",
					When:      "SMTP configured for sending emails",
					Data:      "Email address, message content",
				},
				{
					Condition: "user_initiated",
					When:      "User explicitly shares content (social buttons, exports)",
					Data:      "Whatever user chooses to share",
				},
			},
		},
		Retention: PrivacyRetentionConfig{
			Period:            "Account data is retained while your account is active. Upon account deletion, all personal data is permanently deleted within 30 days. Anonymized analytics data may be retained for up to 12 months.",
			ExportAvailable:   true,
			DeletionAvailable: true,
		},
		Consent: PrivacyConsentConfig{
			ShowUntilAcknowledged: true,
			DefaultEnabled:        true,
			Message:               "In accordance with the EU GDPR law this message is being displayed. We use cookies for essential site functionality and, with your consent, for preferences and analytics. Your data is stored on our servers and is never sold.",
			MessageIfSold:         "In accordance with the EU GDPR law this message is being displayed. We use cookies for essential site functionality and, with your consent, for preferences and analytics. Your data may be shared with or sold to third parties as described in our Privacy Policy.",
			Policy: PrivacyPolicyLinkConfig{
				Text: "Privacy Policy",
				URL:  "/server/privacy",
			},
			Buttons: PrivacyConsentButtonConfig{
				Decline: "Decline",
				Accept:  "I Agree",
			},
			Position:        "bottom",
			ShowPreferences: true,
			PreferencesText: "Manage Preferences",
		},
		Cookies: PrivacyCookiesConfig{
			Essential: PrivacyCookieCategoryConfig{
				Enabled:     true,
				Description: "Required for the site to function. Includes security tokens (CSRF) and site preferences. These cookies are strictly necessary and cannot be disabled.",
			},
			Preferences: PrivacyCookieCategoryConfig{
				Enabled:     true,
				Description: "Remember your settings such as theme (dark/light), language, and UI preferences. Disabling will reset to defaults on each visit.",
			},
			Analytics: PrivacyAnalyticsCookieConfig{
				Enabled:                  true,
				Description:              "Help us understand how visitors use our site to improve the experience.",
				DescriptionSuffixNotSold: "Analytics data is anonymized and never sold.",
				DescriptionSuffixSold:    "Analytics data may be shared with third parties.",
			},
		},
		ThirdParty: PrivacyThirdPartyConfig{
			Services: []PrivacyThirdPartyService{},
		},
		Content: PrivacyContentConfig{
			DataCollection:  defaultPrivacyDataCollection,
			DataUsage:       defaultPrivacyDataUsage,
			DataUsageIfSold: defaultPrivacyDataUsageIfSold,
			DataSecurity:    defaultPrivacyDataSecurity,
		},
	}
}

// The four privacy page sections below are the PART 12 defaults; operators
// override them in server.yml.
const defaultPrivacyDataCollection = `**We collect only what is necessary to provide our service:**

**Usage Information (with consent):**
- Pages visited and features used
- Browser type and device information
- Approximate location (country/region from IP, not precise)

**Technical Information:**
- IP address (for security and abuse prevention)
- API token identifiers (hashed, never stored in plain text)

**We do NOT collect:**
- Payment information (unless explicitly required by the service)
- Precise location data
- Data from other websites or apps
`

const defaultPrivacyDataUsage = `**Your data is used solely to:**

- **Provide the service:** Authentication, core functionality
- **Improve the experience:** Performance optimization, bug fixes, feature improvements
- **Ensure security:** Prevent abuse, detect fraud, protect your account
- **Communicate:** Service updates, security alerts, and (with consent) product news

**Your data is NEVER:**
- Sold to third parties
- Used for targeted advertising
- Shared without your explicit consent (except as required by law)
`

const defaultPrivacyDataUsageIfSold = `**Your data may be used to:**

- **Provide the service:** Authentication, core functionality
- **Improve the experience:** Performance optimization, bug fixes, feature improvements
- **Ensure security:** Prevent abuse, detect fraud, protect your account
- **Communicate:** Service updates, security alerts, and (with consent) product news
- **Third-party sharing:** Your data may be shared with or sold to third parties for analytics, advertising, or other purposes as described below

**Your rights:**
- You can opt out of data sales via your account privacy settings
- You can request deletion of your data at any time
- See "Your Rights" section below for details
`

const defaultPrivacyDataSecurity = `**How we protect your data:**

- All data is stored on our servers (not third-party cloud services unless specified)
- API tokens are stored as SHA-256 hashes (never in plain text)
- All connections are encrypted (HTTPS/TLS)
- Regular security audits and updates
- Access controls and audit logging for operator actions
`
