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
		return defaultPrivacyDataUsageIfSold
	}
	return defaultPrivacyDataUsage
}

// DataCollectionContent returns the project's built-in collection notice.
func (PrivacyConfig) DataCollectionContent() string { return defaultPrivacyDataCollection }

// DataSecurityContent returns the project's built-in security notice.
func (PrivacyConfig) DataSecurityContent() string { return defaultPrivacyDataSecurity }

// defaultPrivacyConfig combines the shared PART 12 behavior defaults with
// the data-handling facts of this project.
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
			Period:            "User-submitted utility inputs are processed to provide a response and are not retained as user records. Operational logs follow the deployment operator's configured retention policy.",
			ExportAvailable:   false,
			DeletionAvailable: false,
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
	}
}

// These privacy sections describe the application's actual data handling and
// are built-in project content; deployment facts remain operator-configured.
const defaultPrivacyDataCollection = `**We collect only what is necessary to provide our service:**

Utility inputs are processed to produce the result requested by the visitor. They are not retained as user records.

**Technical Information:**
- Request source information is processed for rate limiting, abuse prevention and, when enabled, GeoIP features.
- Operational request metadata may be written to server logs according to the deployment configuration.

Some tools make user-requested lookups through third-party services. The relevant tool documentation identifies those services and the query data sent.
`

const defaultPrivacyDataUsage = `**Your data is used solely to:**

- **Provide requested results:** Process the input for the selected utility.
- **Operate and protect the service:** Apply rate limits, prevent abuse and maintain service reliability.

The application does not create user accounts or retain submitted utility inputs as user records. A visitor-requested lookup may send its query to the provider described by that tool.
`

const defaultPrivacyDataUsageIfSold = `**Your data may be used to:**

- **Provide requested results:** Process the input for the selected utility.
- **Operate and protect the service:** Apply rate limits, prevent abuse and maintain service reliability.
- **Third-party sharing:** Data may be shared or sold under the deployment's configured policy, described below.

**Your rights:**
- The privacy page shows the deployment's available export and deletion options.
- When enabled, use the Do Not Sell or Share control in Preferences to opt out.
`

const defaultPrivacyDataSecurity = `**How we protect your data:**

- Submitted utility inputs are processed for the requested response and are not retained as user records.
- Request handling, logging and network encryption depend on the configuration of the service operator's deployment.
- The server applies input validation, request limits and security controls defined by the project.
`
