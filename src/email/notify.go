package email

import (
	"fmt"
	"strconv"
	"sync"
	"time"
)

// GlobalVars holds the values substituted into every template's global
// "{variable}" tokens (AI.md PART 17 "Global Variables"). Callers build
// this once at startup (and again on FQDN/Tor/I2P change) from whatever
// composition-root packages know app identity and overlay addresses -
// src/email has no dependency on those packages itself.
type GlobalVars struct {
	AppName             string
	AppURL              string
	FQDN                string
	OnionURL            string
	OnionAddress        string
	I2PURL              string
	I2PAddress          string
	NotificationReplyTo string
}

// toMap renders GlobalVars, plus the always-computed {timestamp}/{year},
// into the substitution map RenderTemplate expects.
func (g GlobalVars) toMap() map[string]string {
	now := nowFunc()
	return map[string]string{
		"app_name":              g.AppName,
		"app_url":               g.AppURL,
		"fqdn":                  g.FQDN,
		"onion_url":             g.OnionURL,
		"onion_address":         g.OnionAddress,
		"i2p_url":               g.I2PURL,
		"i2p_address":           g.I2PAddress,
		"notification_reply_to": g.NotificationReplyTo,
		"timestamp":             now.Format("2006-01-02 15:04:05 MST"),
		"year":                  strconv.Itoa(now.Year()),
	}
}

// EventToggles mirrors server.notifications.email.events.* (AI.md
// PART 17 "Configuration") - which operator events may be emailed.
// Populate it from config.EmailEventsConfig; kept independent of the
// config package here so src/email carries no import-cycle risk.
type EventToggles struct {
	Startup          bool
	Shutdown         bool
	BackupComplete   bool
	BackupFailed     bool
	SSLExpiring      bool
	SSLRenewed       bool
	SSLRenewalFailed bool
	SecurityAlert    bool
	SchedulerError   bool
	UpdateAvailable  bool
	UpdateInstalled  bool
}

// Notifier dispatches operator notification emails for the 9 events plus
// the manual "test" template (AI.md PART 17 "Default Templates"). It
// owns only the send decision (toggle check, suppression, SMTP
// availability) and the actual send - the structured, leveled log line
// AI.md PART 17 requires for every operator event ("Log the event -
// always") is the caller's responsibility, since logging happens
// regardless of whether email is attempted or SMTP is even configured.
type Notifier struct {
	mu         sync.Mutex
	client     *Client
	configDir  string
	vars       GlobalVars
	toggles    EventToggles
	recipients []string
	suppressed map[string]time.Time
}

// suppressionTTL bounds how long an unconsumed suppression mark is kept.
// A scheduled execution that records a dedicated failure but never
// reaches the scheduler_error stage (process restart, task abandoned)
// would otherwise leave its mark in the map forever.
const suppressionTTL = time.Hour

// NewNotifier builds a Notifier. client may be nil (SMTP unavailable);
// every Notify* method becomes a no-op in that case rather than erroring,
// matching AI.md PART 17 "Completely disable email features if no SMTP".
// recipients is the operator address list used whenever a Notify* call
// passes no explicit recipients, so subsystems (scheduler, backup, ssl,
// update) can fire notifications without resolving contact config
// themselves.
func NewNotifier(client *Client, configDir string, vars GlobalVars, toggles EventToggles, recipients []string) *Notifier {
	return &Notifier{
		client:     client,
		configDir:  configDir,
		vars:       vars,
		toggles:    toggles,
		recipients: append([]string(nil), recipients...),
		suppressed: make(map[string]time.Time),
	}
}

// defaultNotifier holds the process-wide Notifier, registered once from
// the composition root so subsystems that cannot import it (scheduler,
// backup, ssl, update) can still emit operator notifications. Nil means
// no notifier was ever registered; every Notify* method is nil-receiver
// safe, so callers may write email.DefaultNotifier().NotifyX(...)
// unconditionally.
var (
	defaultNotifierMu sync.RWMutex
	defaultNotifier   *Notifier
)

// SetDefaultNotifier registers n as the process-wide Notifier.
func SetDefaultNotifier(n *Notifier) {
	defaultNotifierMu.Lock()
	defer defaultNotifierMu.Unlock()
	defaultNotifier = n
}

// DefaultNotifier returns the process-wide Notifier, or nil when none was
// registered. The returned value is always safe to call methods on.
func DefaultNotifier() *Notifier {
	defaultNotifierMu.RLock()
	defer defaultNotifierMu.RUnlock()
	return defaultNotifier
}

// Enabled reports whether this Notifier can actually deliver mail. Use it
// to decide whether to expose any email-dependent option, per AI.md
// PART 17 "Hide email-dependent UI elements when SMTP unavailable".
func (n *Notifier) Enabled() bool {
	if n == nil {
		return false
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.client != nil && n.client.config.Enabled
}

// SetClient swaps the SMTP client, for use after a config reload changes
// SMTP settings or a startup connection test flips availability.
func (n *Notifier) SetClient(client *Client) {
	if n == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.client = client
}

// SetVars replaces the global template variables, for use when the FQDN,
// branding, or the Tor/I2P overlay addresses become known or change.
func (n *Notifier) SetVars(vars GlobalVars) {
	if n == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.vars = vars
}

// SetToggles replaces the per-event email toggles after a config reload.
func (n *Notifier) SetToggles(toggles EventToggles) {
	if n == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.toggles = toggles
}

// SetRecipients replaces the default operator recipient list.
func (n *Notifier) SetRecipients(recipients []string) {
	if n == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.recipients = append([]string(nil), recipients...)
}

// Recipients returns a copy of the default operator recipient list.
func (n *Notifier) Recipients() []string {
	if n == nil {
		return nil
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.recipients...)
}

// suppressSchedulerError records that executionID's scheduler_error
// notification must be skipped, per AI.md PART 17's suppression rule:
// "backup_failed suppresses scheduler_error for the same execution" /
// "ssl_renewal_failed suppresses scheduler_error for the same execution".
// A blank executionID (event not tied to a scheduled run) is a no-op.
func (n *Notifier) suppressSchedulerError(executionID string) {
	if n == nil || executionID == "" {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.pruneSuppressedLocked()
	n.suppressed[executionID] = nowFunc()
}

// pruneSuppressedLocked drops suppression marks older than suppressionTTL.
// The caller must hold n.mu.
func (n *Notifier) pruneSuppressedLocked() {
	cutoff := nowFunc().Add(-suppressionTTL)
	for id, at := range n.suppressed {
		if at.Before(cutoff) {
			delete(n.suppressed, id)
		}
	}
}

// consumeSuppression reports whether executionID's scheduler_error was
// suppressed by an earlier, more specific failure notification, clearing
// the mark so it cannot leak into an unrelated later execution that
// happens to reuse the same id.
func (n *Notifier) consumeSuppression(executionID string) bool {
	if n == nil || executionID == "" {
		return false
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.pruneSuppressedLocked()
	if _, ok := n.suppressed[executionID]; ok {
		delete(n.suppressed, executionID)
		return true
	}
	return false
}

// send loads templateName (custom override or embedded default), renders
// it against GlobalVars merged with the event-specific vars, and sends it
// to "to". It is a no-op, not an error, when SMTP is unavailable, per
// AI.md PART 17 "DO NOT attempt to send emails without valid SMTP".
func (n *Notifier) send(templateName string, vars map[string]string, to []string) error {
	if n == nil {
		return nil
	}
	n.mu.Lock()
	client := n.client
	configDir := n.configDir
	globals := n.vars.toMap()
	if len(to) == 0 {
		to = append([]string(nil), n.recipients...)
	}
	n.mu.Unlock()

	if client == nil || !client.config.Enabled || len(to) == 0 {
		return nil
	}
	tmpl, err := LoadTemplate(templateName, configDir)
	if err != nil {
		return fmt.Errorf("load template %q: %w", templateName, err)
	}
	for k, v := range vars {
		globals[k] = v
	}
	subject, body := RenderTemplate(tmpl, globals)
	return client.Send(Message{To: to, Subject: subject, Body: body})
}

// NotifyTemplate renders and sends any built-in or operator-overridden
// template by name, bypassing the per-event toggles. It is the escape
// hatch for operator events that share a template with another event
// (AI.md PART 17's Operator Notifications matrix routes "Disk space low"
// and "Database connection issue" through security_alert, for example).
// Passing no recipients uses the Notifier's default operator list.
func (n *Notifier) NotifyTemplate(templateName string, vars map[string]string, to []string) error {
	return n.send(templateName, vars, to)
}

// eventEnabled reports whether the named per-event toggle permits email.
func (n *Notifier) eventEnabled(pick func(EventToggles) bool) bool {
	if n == nil {
		return false
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	return pick(n.toggles)
}

// NotifySecurityAlert sends the security_alert template (AI.md PART 17).
func (n *Notifier) NotifySecurityAlert(to []string, event, ip, details string) error {
	if !n.eventEnabled(func(t EventToggles) bool { return t.SecurityAlert }) {
		return nil
	}
	return n.send("security_alert", map[string]string{
		"event": event, "ip": ip, "details": details,
	}, to)
}

// NotifyBackupComplete sends the backup_complete template.
func (n *Notifier) NotifyBackupComplete(to []string, filename, size string) error {
	if !n.eventEnabled(func(t EventToggles) bool { return t.BackupComplete }) {
		return nil
	}
	return n.send("backup_complete", map[string]string{
		"filename": filename, "size": size,
	}, to)
}

// NotifyBackupFailed sends the backup_failed template and suppresses the
// scheduler_error notification for the same executionID.
func (n *Notifier) NotifyBackupFailed(executionID string, to []string, filename, size, errMsg string) error {
	n.suppressSchedulerError(executionID)
	if !n.eventEnabled(func(t EventToggles) bool { return t.BackupFailed }) {
		return nil
	}
	return n.send("backup_failed", map[string]string{
		"filename": filename, "size": size, "error": errMsg,
	}, to)
}

// NotifySSLExpiring sends the ssl_expiring template. Per AI.md PART 17's
// Operator Notifications matrix, only the 7/3/1-day warnings should ever
// reach this method - the 30/14-day early warnings are log-only and must
// not be passed through to email.
func (n *Notifier) NotifySSLExpiring(to []string, fqdn, expiresIn, expiryDate string) error {
	if !n.eventEnabled(func(t EventToggles) bool { return t.SSLExpiring }) {
		return nil
	}
	return n.send("ssl_expiring", map[string]string{
		"fqdn": fqdn, "expires_in": expiresIn, "expiry_date": expiryDate,
	}, to)
}

// NotifySSLRenewed sends the ssl_renewed template.
func (n *Notifier) NotifySSLRenewed(to []string, fqdn, validUntil string) error {
	if !n.eventEnabled(func(t EventToggles) bool { return t.SSLRenewed }) {
		return nil
	}
	return n.send("ssl_renewed", map[string]string{
		"fqdn": fqdn, "valid_until": validUntil,
	}, to)
}

// NotifySSLRenewalFailed sends the ssl_renewal_failed template and
// suppresses the scheduler_error notification for the same executionID.
func (n *Notifier) NotifySSLRenewalFailed(executionID string, to []string, fqdn, errMsg, expiresIn, expiryDate, nextRetry string) error {
	n.suppressSchedulerError(executionID)
	if !n.eventEnabled(func(t EventToggles) bool { return t.SSLRenewalFailed }) {
		return nil
	}
	return n.send("ssl_renewal_failed", map[string]string{
		"fqdn": fqdn, "error": errMsg, "expires_in": expiresIn,
		"expiry_date": expiryDate, "next_retry": nextRetry,
	}, to)
}

// NotifySchedulerError sends the scheduler_error template, unless a more
// specific failure notification (backup_failed, ssl_renewal_failed) has
// already suppressed it for the same executionID (AI.md PART 17
// "Suppression"). Tasks with no dedicated failure event of their own
// (token_cleanup, log_rotation, update_check) should call this with a
// fresh, never-suppressed executionID so they always notify normally.
func (n *Notifier) NotifySchedulerError(executionID string, to []string, taskName, errMsg, nextRun string) error {
	if n.consumeSuppression(executionID) {
		return nil
	}
	if !n.eventEnabled(func(t EventToggles) bool { return t.SchedulerError }) {
		return nil
	}
	return n.send("scheduler_error", map[string]string{
		"task_name": taskName, "error": errMsg, "next_run": nextRun,
	}, to)
}

// NotifyUpdateAvailable sends the update_available template.
func (n *Notifier) NotifyUpdateAvailable(to []string, currentVersion, newVersion, channel string) error {
	if !n.eventEnabled(func(t EventToggles) bool { return t.UpdateAvailable }) {
		return nil
	}
	return n.send("update_available", map[string]string{
		"current_version": currentVersion, "new_version": newVersion, "channel": channel,
	}, to)
}

// NotifyUpdateInstalled sends the update_installed template.
func (n *Notifier) NotifyUpdateInstalled(to []string, previousVersion, newVersion string) error {
	if !n.eventEnabled(func(t EventToggles) bool { return t.UpdateInstalled }) {
		return nil
	}
	return n.send("update_installed", map[string]string{
		"previous_version": previousVersion, "new_version": newVersion,
	}, to)
}

// SendTestEmail sends the test template to "to", subject prefixed
// "[TEST]" per AI.md PART 17 "Send Test Email". Unlike the Notify*
// methods it always attempts to send (bypassing event toggles, since it
// is an explicit manual operator action) but still requires SMTP to be
// enabled, returning an error rather than silently no-op'ing so
// "{project_name} email test" can report failure to the caller.
func (n *Notifier) SendTestEmail(to []string) error {
	if n == nil {
		return fmt.Errorf("email is not enabled")
	}
	n.mu.Lock()
	client := n.client
	configDir := n.configDir
	vars := n.vars.toMap()
	if len(to) == 0 {
		to = append([]string(nil), n.recipients...)
	}
	n.mu.Unlock()

	if client == nil || !client.config.Enabled {
		return fmt.Errorf("email is not enabled")
	}
	if len(to) == 0 {
		return fmt.Errorf("no recipient for test email")
	}
	tmpl, err := LoadTemplate("test", configDir)
	if err != nil {
		return fmt.Errorf("load template %q: %w", "test", err)
	}
	subject, body := RenderTemplate(tmpl, vars)
	return client.Send(Message{To: to, Subject: "[TEST] " + subject, Body: body})
}
