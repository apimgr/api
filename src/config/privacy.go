package config

import "strings"

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
	Period            any  `yaml:"period"`
	ExportAvailable   bool `yaml:"export_available"`
	DeletionAvailable bool `yaml:"deletion_available"`
}

// PeriodFor returns an operator-supplied retention statement for the request
// language, falling back to the configured default. An empty result selects
// the built-in translated statement.
func (p PrivacyRetentionConfig) PeriodFor(language string) string {
	switch period := p.Period.(type) {
	case string:
		return period
	case map[string]any:
		if value, ok := period[strings.ToLower(language)].(string); ok && value != "" {
			return value
		}
		if value, ok := period["default"].(string); ok {
			return value
		}
	case map[string]string:
		if value := period[strings.ToLower(language)]; value != "" {
			return value
		}
		return period["default"]
	}
	return ""
}

// PrivacyConsentConfig configures the cookie consent banner.
type PrivacyConsentConfig struct {
	ShowUntilAcknowledged bool                       `yaml:"show_until_acknowledged"`
	Message               string                     `yaml:"message"`
	MessageIfSold         string                     `yaml:"message_if_sold"`
	Policy                PrivacyPolicyLinkConfig    `yaml:"policy"`
	Buttons               PrivacyConsentButtonConfig `yaml:"buttons"`
	ShowPreferences       bool                       `yaml:"show_preferences"`
	PreferencesText       string                     `yaml:"preferences_text"`
}

// DefaultPrivacyConsentMessage values are operator-configurable English
// defaults whose display falls back to localized strings in the web UI.
const (
	DefaultPrivacyConsentMessage               = "In accordance with the EU GDPR law this message is being displayed. We use cookies for essential site functionality and, with your consent, for preferences and analytics. Your data is stored on our servers and is never sold."
	DefaultPrivacyConsentMessageIfSold         = "In accordance with the EU GDPR law this message is being displayed. We use cookies for essential site functionality and, with your consent, for preferences and analytics. Your data may be shared with or sold to third parties as described in our Privacy Policy."
	DefaultPrivacyConsentPolicyText            = "Privacy Policy"
	DefaultPrivacyConsentAcceptText            = "I Agree"
	DefaultPrivacyConsentDeclineText           = "Decline"
	DefaultPrivacyConsentPreferencesText       = "Manage Preferences"
	DefaultPrivacyEssentialCookieDescription   = "Required for the site to function. Includes security tokens (CSRF) and site preferences. These cookies are strictly necessary and cannot be disabled."
	DefaultPrivacyPreferencesCookieDescription = "Remember your settings such as theme (dark/light), language, and UI preferences. Disabling will reset to defaults on each visit."
	DefaultPrivacyAnalyticsCookieDescription   = "Help us understand how visitors use our site to improve the experience."
	DefaultPrivacyAnalyticsSuffixNotSold       = "Analytics data is anonymized and never sold."
	DefaultPrivacyAnalyticsSuffixSold          = "Analytics data may be shared with third parties."
)

// PrivacyPolicyLinkConfig is the consent banner's privacy policy link.
type PrivacyPolicyLinkConfig struct {
	Text    string `yaml:"text"`
	URL     string `yaml:"url"`
	Version string `yaml:"version"`
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
			Period:            "",
			ExportAvailable:   false,
			DeletionAvailable: false,
		},
		Consent: PrivacyConsentConfig{
			ShowUntilAcknowledged: true,
			Message:               DefaultPrivacyConsentMessage,
			MessageIfSold:         DefaultPrivacyConsentMessageIfSold,
			Policy: PrivacyPolicyLinkConfig{
				Text:    DefaultPrivacyConsentPolicyText,
				URL:     "/server/privacy",
				Version: "1",
			},
			Buttons: PrivacyConsentButtonConfig{
				Decline: DefaultPrivacyConsentDeclineText,
				Accept:  DefaultPrivacyConsentAcceptText,
			},
			ShowPreferences: true,
			PreferencesText: DefaultPrivacyConsentPreferencesText,
		},
		Cookies: PrivacyCookiesConfig{
			Essential: PrivacyCookieCategoryConfig{
				Enabled:     true,
				Description: DefaultPrivacyEssentialCookieDescription,
			},
			Preferences: PrivacyCookieCategoryConfig{
				Enabled:     true,
				Description: DefaultPrivacyPreferencesCookieDescription,
			},
			Analytics: PrivacyAnalyticsCookieConfig{
				Enabled:                  true,
				Description:              DefaultPrivacyAnalyticsCookieDescription,
				DescriptionSuffixNotSold: DefaultPrivacyAnalyticsSuffixNotSold,
				DescriptionSuffixSold:    DefaultPrivacyAnalyticsSuffixSold,
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
