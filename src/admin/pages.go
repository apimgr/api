package admin

import (
	"fmt"

	"github.com/apimgr/api/src/config"
)

// Admin page templates - dark theme (Dracula) per AI.md

const adminBaseCSS = `
<style>
:root {
  --bg-primary: #282a36;
  --bg-secondary: #1e1f29;
  --bg-tertiary: #44475a;
  --text-primary: #f8f8f2;
  --text-secondary: #6272a4;
  --accent: #bd93f9;
  --accent-hover: #ff79c6;
  --success: #50fa7b;
  --warning: #ffb86c;
  --error: #ff5555;
  --info: #8be9fd;
  --border: #44475a;
}

* {
  box-sizing: border-box;
  margin: 0;
  padding: 0;
}

body {
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
  background: var(--bg-primary);
  color: var(--text-primary);
  line-height: 1.6;
}

.admin-layout {
  display: flex;
  min-height: 100vh;
}

.sidebar {
  width: 250px;
  background: var(--bg-secondary);
  padding: 1rem;
  border-right: 1px solid var(--border);
}

.sidebar-header {
  padding: 1rem;
  border-bottom: 1px solid var(--border);
  margin-bottom: 1rem;
}

.sidebar-header h1 {
  font-size: 1.25rem;
  color: var(--accent);
}

.sidebar-nav {
  list-style: none;
}

.sidebar-nav a {
  display: block;
  padding: 0.75rem 1rem;
  color: var(--text-primary);
  text-decoration: none;
  border-radius: 0.5rem;
  margin-bottom: 0.25rem;
  transition: background 0.2s;
}

.sidebar-nav a:hover {
  background: var(--bg-tertiary);
}

.sidebar-nav a.active {
  background: var(--accent);
  color: var(--bg-primary);
}

.main-content {
  flex: 1;
  padding: 2rem;
  max-width: 1200px;
}

.page-header {
  margin-bottom: 2rem;
}

.page-header h1 {
  font-size: 2rem;
  margin-bottom: 0.5rem;
}

.page-header p {
  color: var(--text-secondary);
}

.card {
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  border-radius: 0.5rem;
  padding: 1.5rem;
  margin-bottom: 1.5rem;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 1rem;
  padding-bottom: 1rem;
  border-bottom: 1px solid var(--border);
}

.card-header h2 {
  font-size: 1.25rem;
}

.form-group {
  margin-bottom: 1rem;
}

.form-group label {
  display: block;
  margin-bottom: 0.5rem;
  color: var(--text-secondary);
  font-size: 0.875rem;
}

.form-control {
  width: 100%;
  padding: 0.75rem;
  background: var(--bg-primary);
  border: 1px solid var(--border);
  border-radius: 0.375rem;
  color: var(--text-primary);
  font-size: 1rem;
}

.form-control:focus {
  outline: none;
  border-color: var(--accent);
}

.btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 0.75rem 1.5rem;
  border: none;
  border-radius: 0.375rem;
  font-size: 0.875rem;
  font-weight: 500;
  cursor: pointer;
  transition: background 0.2s;
  text-decoration: none;
}

.btn-primary {
  background: var(--accent);
  color: var(--bg-primary);
}

.btn-primary:hover {
  background: var(--accent-hover);
}

.btn-secondary {
  background: var(--bg-tertiary);
  color: var(--text-primary);
}

.btn-secondary:hover {
  background: var(--border);
}

.btn-danger {
  background: var(--error);
  color: var(--text-primary);
}

.stats-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 1rem;
  margin-bottom: 2rem;
}

.stat-card {
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  border-radius: 0.5rem;
  padding: 1.5rem;
}

.stat-card h3 {
  font-size: 0.875rem;
  color: var(--text-secondary);
  margin-bottom: 0.5rem;
}

.stat-card .value {
  font-size: 2rem;
  font-weight: 600;
  color: var(--accent);
}

.alert {
  padding: 1rem;
  border-radius: 0.375rem;
  margin-bottom: 1rem;
}

.alert-success {
  background: rgba(80, 250, 123, 0.1);
  border: 1px solid var(--success);
  color: var(--success);
}

.alert-error {
  background: rgba(255, 85, 85, 0.1);
  border: 1px solid var(--error);
  color: var(--error);
}

.table {
  width: 100%;
  border-collapse: collapse;
}

.table th,
.table td {
  padding: 0.75rem;
  text-align: left;
  border-bottom: 1px solid var(--border);
}

.table th {
  color: var(--text-secondary);
  font-weight: 500;
}

.badge {
  display: inline-flex;
  align-items: center;
  padding: 0.25rem 0.5rem;
  border-radius: 9999px;
  font-size: 0.75rem;
  font-weight: 500;
}

.badge-success {
  background: rgba(80, 250, 123, 0.2);
  color: var(--success);
}

.badge-warning {
  background: rgba(255, 184, 108, 0.2);
  color: var(--warning);
}

.badge-error {
  background: rgba(255, 85, 85, 0.2);
  color: var(--error);
}
</style>
`

const adminLoginCSS = `
<style>
body {
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
  background: #282a36;
  color: #f8f8f2;
  display: flex;
  justify-content: center;
  align-items: center;
  min-height: 100vh;
  margin: 0;
}

.login-container {
  width: 100%;
  max-width: 400px;
  padding: 2rem;
}

.login-card {
  background: #1e1f29;
  border: 1px solid #44475a;
  border-radius: 0.5rem;
  padding: 2rem;
}

.login-header {
  text-align: center;
  margin-bottom: 2rem;
}

.login-header h1 {
  font-size: 1.5rem;
  color: #bd93f9;
  margin-bottom: 0.5rem;
}

.login-header p {
  color: #6272a4;
}

.form-group {
  margin-bottom: 1rem;
}

.form-group label {
  display: block;
  margin-bottom: 0.5rem;
  color: #6272a4;
  font-size: 0.875rem;
}

.form-control {
  width: 100%;
  padding: 0.75rem;
  background: #282a36;
  border: 1px solid #44475a;
  border-radius: 0.375rem;
  color: #f8f8f2;
  font-size: 1rem;
}

.form-control:focus {
  outline: none;
  border-color: #bd93f9;
}

.btn {
  width: 100%;
  padding: 0.75rem;
  background: #bd93f9;
  color: #282a36;
  border: none;
  border-radius: 0.375rem;
  font-size: 1rem;
  font-weight: 500;
  cursor: pointer;
  transition: background 0.2s;
}

.btn:hover {
  background: #ff79c6;
}

.alert {
  padding: 0.75rem;
  border-radius: 0.375rem;
  margin-bottom: 1rem;
  font-size: 0.875rem;
}

.alert-error {
  background: rgba(255, 85, 85, 0.1);
  border: 1px solid #ff5555;
  color: #ff5555;
}

.login-footer {
  text-align: center;
  margin-top: 1.5rem;
}

.login-footer a {
  color: #bd93f9;
  text-decoration: none;
}
</style>
`

func generateLoginPage(csrfToken string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Admin Login - CasTools</title>
  %s
</head>
<body>
  <div class="login-container">
    <div class="login-card">
      <div class="login-header">
        <h1>Admin Login</h1>
        <p>Enter your credentials to continue</p>
      </div>
      <form method="POST" action="/admin/login">
        <input type="hidden" name="csrf_token" value="%s">
        <div class="form-group">
          <label for="username">Username</label>
          <input type="text" id="username" name="username" class="form-control" required autofocus>
        </div>
        <div class="form-group">
          <label for="password">Password</label>
          <input type="password" id="password" name="password" class="form-control" required>
        </div>
        <button type="submit" class="btn">Sign In</button>
      </form>
      <div class="login-footer">
        <a href="/auth/password/forgot">Forgot password?</a>
      </div>
    </div>
  </div>
</body>
</html>`, adminLoginCSS, csrfToken)
}

func generateDashboardPage(cfg *config.Config, csrfToken string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Dashboard - Admin</title>
  %s
</head>
<body>
  <div class="admin-layout">
    %s
    <main class="main-content">
      <div class="page-header">
        <h1>Dashboard</h1>
        <p>Server overview and quick actions</p>
      </div>

      <div class="stats-grid">
        <div class="stat-card">
          <h3>Status</h3>
          <div class="value">Running</div>
        </div>
        <div class="stat-card">
          <h3>Version</h3>
          <div class="value">%s</div>
        </div>
        <div class="stat-card">
          <h3>Mode</h3>
          <div class="value">%s</div>
        </div>
        <div class="stat-card">
          <h3>Port</h3>
          <div class="value">%s</div>
        </div>
      </div>

      <div class="card">
        <div class="card-header">
          <h2>Server Information</h2>
        </div>
        <table class="table">
          <tr>
            <td>FQDN</td>
            <td>%s</td>
          </tr>
          <tr>
            <td>Address</td>
            <td>%s</td>
          </tr>
          <tr>
            <td>Theme</td>
            <td>%s</td>
          </tr>
          <tr>
            <td>SSL Enabled</td>
            <td>%v</td>
          </tr>
        </table>
      </div>

      <div class="card">
        <div class="card-header">
          <h2>Quick Actions</h2>
        </div>
        <div style="display: flex; gap: 1rem;">
          <a href="/admin/server/settings" class="btn btn-secondary">Server Settings</a>
          <a href="/admin/server/logs" class="btn btn-secondary">View Logs</a>
          <a href="/admin/server/backup" class="btn btn-secondary">Create Backup</a>
        </div>
      </div>
    </main>
  </div>
</body>
</html>`, adminBaseCSS, generateSidebar("dashboard"), Version, cfg.Server.Mode, cfg.Server.Port, cfg.Server.FQDN, cfg.Server.Address, cfg.Web.UI.Theme, cfg.Server.SSL.Enabled)
}

func generateSidebar(active string) string {
	items := []struct {
		href  string
		label string
		key   string
	}{
		{"/admin/dashboard", "Dashboard", "dashboard"},
		{"/admin/server/settings", "Server Settings", "settings"},
		{"/admin/server/branding", "Branding & SEO", "branding"},
		{"/admin/server/ssl", "SSL/TLS", "ssl"},
		{"/admin/server/web", "Web Settings", "web"},
		{"/admin/server/email", "Email & SMTP", "email"},
		{"/admin/server/scheduler", "Scheduler", "scheduler"},
		{"/admin/server/backup", "Backup & Restore", "backup"},
		{"/admin/server/logs", "Logs", "logs"},
	}

	nav := `<aside class="sidebar">
    <div class="sidebar-header">
      <h1>CasTools Admin</h1>
    </div>
    <nav>
      <ul class="sidebar-nav">`

	for _, item := range items {
		class := ""
		if item.key == active {
			class = " class=\"active\""
		}
		nav += fmt.Sprintf(`
        <li><a href="%s"%s>%s</a></li>`, item.href, class, item.label)
	}

	nav += `
      </ul>
    </nav>
    <div style="margin-top: auto; padding-top: 2rem; border-top: 1px solid #44475a;">
      <a href="/admin/logout" style="color: #ff5555; text-decoration: none; font-size: 0.875rem;">Sign Out</a>
    </div>
  </aside>`

	return nav
}

func generateSettingsPage(cfg *config.Config, csrfToken string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Server Settings - Admin</title>
  %s
</head>
<body>
  <div class="admin-layout">
    %s
    <main class="main-content">
      <div class="page-header">
        <h1>Server Settings</h1>
        <p>Configure server behavior and network settings</p>
      </div>

      <form method="POST" action="/api/v1/admin/server/settings">
        <input type="hidden" name="csrf_token" value="%s">

        <div class="card">
          <div class="card-header">
            <h2>Network</h2>
          </div>
          <div class="form-group">
            <label for="port">Port</label>
            <input type="text" id="port" name="port" class="form-control" value="%s">
          </div>
          <div class="form-group">
            <label for="address">Listen Address</label>
            <input type="text" id="address" name="address" class="form-control" value="%s">
          </div>
          <div class="form-group">
            <label for="fqdn">FQDN</label>
            <input type="text" id="fqdn" name="fqdn" class="form-control" value="%s">
          </div>
        </div>

        <div class="card">
          <div class="card-header">
            <h2>Rate Limiting</h2>
          </div>
          <div class="form-group">
            <label for="rate_requests">Requests per Window</label>
            <input type="number" id="rate_requests" name="rate_requests" class="form-control" value="%d">
          </div>
          <div class="form-group">
            <label for="rate_window">Window (seconds)</label>
            <input type="number" id="rate_window" name="rate_window" class="form-control" value="%d">
          </div>
        </div>

        <button type="submit" class="btn btn-primary">Save Changes</button>
      </form>
    </main>
  </div>
</body>
</html>`, adminBaseCSS, generateSidebar("settings"), csrfToken, cfg.Server.Port, cfg.Server.Address, cfg.Server.FQDN, cfg.Server.RateLimit.Requests, cfg.Server.RateLimit.Window)
}

func generateBrandingPage(cfg *config.Config, csrfToken string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Branding & SEO - Admin</title>
  %s
</head>
<body>
  <div class="admin-layout">
    %s
    <main class="main-content">
      <div class="page-header">
        <h1>Branding & SEO</h1>
        <p>Customize the appearance and metadata of your site</p>
      </div>

      <form method="POST">
        <input type="hidden" name="csrf_token" value="%s">

        <div class="card">
          <div class="card-header">
            <h2>Site Identity</h2>
          </div>
          <div class="form-group">
            <label for="title">Site Title</label>
            <input type="text" id="title" name="title" class="form-control" value="%s">
          </div>
          <div class="form-group">
            <label for="tagline">Tagline</label>
            <input type="text" id="tagline" name="tagline" class="form-control" value="%s">
          </div>
        </div>

        <div class="card">
          <div class="card-header">
            <h2>Theme</h2>
          </div>
          <div class="form-group">
            <label for="theme">Color Theme</label>
            <select id="theme" name="theme" class="form-control">
              <option value="dark" %s>Dark (Dracula)</option>
              <option value="light" %s>Light</option>
            </select>
          </div>
        </div>

        <button type="submit" class="btn btn-primary">Save Changes</button>
      </form>
    </main>
  </div>
</body>
</html>`, adminBaseCSS, generateSidebar("branding"), csrfToken, cfg.Server.Branding.Title, cfg.Server.Branding.Tagline, selected(cfg.Web.UI.Theme, "dark"), selected(cfg.Web.UI.Theme, "light"))
}

func generateSSLPage(cfg *config.Config, csrfToken string) string {
	sslStatus := "Disabled"
	sslBadge := "badge-error"
	if cfg.Server.SSL.Enabled {
		sslStatus = "Enabled"
		sslBadge = "badge-success"
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>SSL/TLS - Admin</title>
  %s
</head>
<body>
  <div class="admin-layout">
    %s
    <main class="main-content">
      <div class="page-header">
        <h1>SSL/TLS Settings</h1>
        <p>Configure HTTPS and certificate management</p>
      </div>

      <div class="card">
        <div class="card-header">
          <h2>Status</h2>
          <span class="badge %s">%s</span>
        </div>
        <p>SSL/TLS encryption for secure connections.</p>
      </div>

      <form method="POST">
        <input type="hidden" name="csrf_token" value="%s">

        <div class="card">
          <div class="card-header">
            <h2>SSL Configuration</h2>
          </div>
          <div class="form-group">
            <label>
              <input type="checkbox" name="ssl_enabled" %s> Enable SSL/TLS
            </label>
          </div>
        </div>

        <div class="card">
          <div class="card-header">
            <h2>Let's Encrypt</h2>
          </div>
          <div class="form-group">
            <label>
              <input type="checkbox" name="le_enabled" %s> Enable Let's Encrypt
            </label>
          </div>
          <div class="form-group">
            <label for="le_email">Email for Let's Encrypt</label>
            <input type="email" id="le_email" name="le_email" class="form-control" value="%s">
          </div>
          <div class="form-group">
            <label for="le_challenge">Challenge Type</label>
            <select id="le_challenge" name="le_challenge" class="form-control">
              <option value="http-01" %s>HTTP-01</option>
              <option value="tls-alpn-01" %s>TLS-ALPN-01</option>
              <option value="dns-01" %s>DNS-01</option>
            </select>
          </div>
        </div>

        <button type="submit" class="btn btn-primary">Save Changes</button>
      </form>
    </main>
  </div>
</body>
</html>`, adminBaseCSS, generateSidebar("ssl"), sslBadge, sslStatus, csrfToken, checked(cfg.Server.SSL.Enabled), checked(cfg.Server.SSL.LetsEncrypt.Enabled), cfg.Server.SSL.LetsEncrypt.Email, selected(cfg.Server.SSL.LetsEncrypt.Challenge, "http-01"), selected(cfg.Server.SSL.LetsEncrypt.Challenge, "tls-alpn-01"), selected(cfg.Server.SSL.LetsEncrypt.Challenge, "dns-01"))
}

func generateWebSettingsPage(cfg *config.Config, csrfToken string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Web Settings - Admin</title>
  %s
</head>
<body>
  <div class="admin-layout">
    %s
    <main class="main-content">
      <div class="page-header">
        <h1>Web Settings</h1>
        <p>Configure robots.txt and security.txt</p>
      </div>

      <form method="POST">
        <input type="hidden" name="csrf_token" value="%s">

        <div class="card">
          <div class="card-header">
            <h2>robots.txt</h2>
          </div>
          <div class="form-group">
            <label for="robots_allow">Allow Paths (one per line)</label>
            <textarea id="robots_allow" name="robots_allow" class="form-control" rows="3">%s</textarea>
          </div>
          <div class="form-group">
            <label for="robots_deny">Deny Paths (one per line)</label>
            <textarea id="robots_deny" name="robots_deny" class="form-control" rows="3">%s</textarea>
          </div>
        </div>

        <div class="card">
          <div class="card-header">
            <h2>security.txt</h2>
          </div>
          <div class="form-group">
            <label for="security_contact">Security Contact Email</label>
            <input type="email" id="security_contact" name="security_contact" class="form-control" value="%s">
          </div>
          <div class="form-group">
            <label for="security_expires">Expires</label>
            <input type="date" id="security_expires" name="security_expires" class="form-control" value="%s">
          </div>
        </div>

        <div class="card">
          <div class="card-header">
            <h2>CORS</h2>
          </div>
          <div class="form-group">
            <label for="cors">Allowed Origins</label>
            <input type="text" id="cors" name="cors" class="form-control" value="%s">
          </div>
        </div>

        <button type="submit" class="btn btn-primary">Save Changes</button>
      </form>
    </main>
  </div>
</body>
</html>`, adminBaseCSS, generateSidebar("web"), csrfToken, joinLines(cfg.Web.Robots.Allow), joinLines(cfg.Web.Robots.Deny), cfg.Web.Security.Contact, cfg.Web.Security.Expires.Format("2006-01-02"), cfg.Web.CORS)
}

func generateEmailPage(cfg *config.Config, csrfToken string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Email Settings - Admin</title>
  %s
</head>
<body>
  <div class="admin-layout">
    %s
    <main class="main-content">
      <div class="page-header">
        <h1>Email & SMTP</h1>
        <p>Configure email notifications and SMTP settings</p>
      </div>

      <div class="card">
        <div class="card-header">
          <h2>SMTP Configuration</h2>
        </div>
        <p style="color: var(--text-secondary);">Email configuration coming soon.</p>
      </div>
    </main>
  </div>
</body>
</html>`, adminBaseCSS, generateSidebar("email"))
}

func generateSchedulerPage(cfg *config.Config, csrfToken string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Scheduler - Admin</title>
  %s
</head>
<body>
  <div class="admin-layout">
    %s
    <main class="main-content">
      <div class="page-header">
        <h1>Scheduler</h1>
        <p>Manage scheduled tasks</p>
      </div>

      <div class="card">
        <div class="card-header">
          <h2>Scheduled Tasks</h2>
        </div>
        <table class="table">
          <thead>
            <tr>
              <th>Task</th>
              <th>Interval</th>
              <th>Last Run</th>
              <th>Next Run</th>
              <th>Status</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td colspan="6" style="color: var(--text-secondary); text-align: center;">No scheduled tasks</td>
            </tr>
          </tbody>
        </table>
      </div>
    </main>
  </div>
</body>
</html>`, adminBaseCSS, generateSidebar("scheduler"))
}

func generateBackupPage(cfg *config.Config, csrfToken string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Backup & Restore - Admin</title>
  %s
</head>
<body>
  <div class="admin-layout">
    %s
    <main class="main-content">
      <div class="page-header">
        <h1>Backup & Restore</h1>
        <p>Create and manage server backups</p>
      </div>

      <div class="card">
        <div class="card-header">
          <h2>Create Backup</h2>
          <button class="btn btn-primary">Create Backup Now</button>
        </div>
        <p style="color: var(--text-secondary);">Backups include configuration, database, and custom assets.</p>
      </div>

      <div class="card">
        <div class="card-header">
          <h2>Available Backups</h2>
        </div>
        <table class="table">
          <thead>
            <tr>
              <th>Filename</th>
              <th>Date</th>
              <th>Size</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td colspan="4" style="color: var(--text-secondary); text-align: center;">No backups available</td>
            </tr>
          </tbody>
        </table>
      </div>
    </main>
  </div>
</body>
</html>`, adminBaseCSS, generateSidebar("backup"))
}

func generateLogsPage(cfg *config.Config, csrfToken string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Logs - Admin</title>
  %s
</head>
<body>
  <div class="admin-layout">
    %s
    <main class="main-content">
      <div class="page-header">
        <h1>Logs</h1>
        <p>View and download server logs</p>
      </div>

      <div class="stats-grid">
        <div class="card">
          <h3 style="margin-bottom: 0.5rem;">Access Log</h3>
          <p style="color: var(--text-secondary); font-size: 0.875rem;">HTTP requests</p>
          <a href="/api/v1/admin/server/logs/access/download" class="btn btn-secondary" style="margin-top: 1rem;">Download</a>
        </div>
        <div class="card">
          <h3 style="margin-bottom: 0.5rem;">Error Log</h3>
          <p style="color: var(--text-secondary); font-size: 0.875rem;">Application errors</p>
          <a href="/api/v1/admin/server/logs/error/download" class="btn btn-secondary" style="margin-top: 1rem;">Download</a>
        </div>
        <div class="card">
          <h3 style="margin-bottom: 0.5rem;">Security Log</h3>
          <p style="color: var(--text-secondary); font-size: 0.875rem;">Auth & security events</p>
          <a href="/api/v1/admin/server/logs/security/download" class="btn btn-secondary" style="margin-top: 1rem;">Download</a>
        </div>
        <div class="card">
          <h3 style="margin-bottom: 0.5rem;">Audit Log</h3>
          <p style="color: var(--text-secondary); font-size: 0.875rem;">Admin actions</p>
          <a href="/api/v1/admin/server/logs/audit/download" class="btn btn-secondary" style="margin-top: 1rem;">Download</a>
        </div>
      </div>
    </main>
  </div>
</body>
</html>`, adminBaseCSS, generateSidebar("logs"))
}

func generateForgotPasswordPage() string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Forgot Password - CasTools</title>
  %s
</head>
<body>
  <div class="login-container">
    <div class="login-card">
      <div class="login-header">
        <h1>Reset Password</h1>
        <p>Password reset is not available for admin accounts.</p>
        <p style="margin-top: 1rem; color: #6272a4;">Check your server.yml configuration file for the admin password.</p>
      </div>
      <div class="login-footer">
        <a href="/admin/login">Back to Login</a>
      </div>
    </div>
  </div>
</body>
</html>`, adminLoginCSS)
}

// Helper functions

func selected(current, value string) string {
	if current == value {
		return "selected"
	}
	return ""
}

func checked(b bool) string {
	if b {
		return "checked"
	}
	return ""
}

func joinLines(items []string) string {
	result := ""
	for i, item := range items {
		if i > 0 {
			result += "\n"
		}
		result += item
	}
	return result
}
