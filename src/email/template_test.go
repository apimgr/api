package email

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDefaultTemplates_AllPresentAndValid asserts every one of the 10
// built-in templates from AI.md PART 17 exists as an embedded default,
// parses, and validates cleanly with zero configuration.
func TestDefaultTemplates_AllPresentAndValid(t *testing.T) {
	require.Len(t, TemplateNames, 10)
	for _, name := range TemplateNames {
		tmpl, err := LoadTemplate(name, t.TempDir())
		require.NoError(t, err, name)
		assert.NotEmpty(t, tmpl.Subject, name)
		assert.NotEmpty(t, strings.TrimSpace(tmpl.Body), name)

		raw := "Subject: " + tmpl.Subject + "\n---\n" + tmpl.Body
		warnings, err := ValidateTemplate(name, []byte(raw))
		require.NoError(t, err, name)
		assert.Empty(t, warnings, name)
	}
}

// TestDefaultTemplates_StateIdentityAndLink checks AI.md PART 17's rule
// that every notification email states the app identity (name + FQDN) and
// carries a visible plaintext link.
func TestDefaultTemplates_StateIdentityAndLink(t *testing.T) {
	for _, name := range TemplateNames {
		tmpl, err := LoadTemplate(name, t.TempDir())
		require.NoError(t, err, name)
		assert.Contains(t, tmpl.Body, "{app_name}", name)
		assert.Contains(t, tmpl.Body, "{fqdn}", name)
		assert.Contains(t, tmpl.Body, "{app_url}", name)
	}
}

func TestParseTemplate(t *testing.T) {
	tmpl, err := ParseTemplate("test", []byte("Subject: Hello {app_name}\n---\nBody line\n"))
	require.NoError(t, err)
	assert.Equal(t, "Hello {app_name}", tmpl.Subject)
	assert.Equal(t, "Body line\n", tmpl.Body)
}

func TestParseTemplate_MissingSubjectAndSeparator(t *testing.T) {
	_, err := ParseTemplate("test", []byte("Hello\n---\nBody\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "line 1")

	_, err = ParseTemplate("test", []byte("Subject: Hi\nBody with no separator\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "line 2")
}

func TestRenderTemplate_LeavesUnknownTokensLiteral(t *testing.T) {
	tmpl := &Template{Name: "test", Subject: "Hi {app_name}", Body: "See {app_url} and {nope}\n"}
	subject, body := RenderTemplate(tmpl, map[string]string{
		"app_name": "api",
		"app_url":  "https://example.com",
	})
	assert.Equal(t, "Hi api", subject)
	assert.Equal(t, "See https://example.com and {nope}\n", body)
}

// TestLoadTemplate_CustomOverridesEmbedded covers the two-tier resolution
// of AI.md PART 17 "Template Storage": a file under
// {config_dir}/template/email/ wins over the embedded default.
func TestLoadTemplate_CustomOverridesEmbedded(t *testing.T) {
	configDir := t.TempDir()

	def, err := LoadTemplate("test", configDir)
	require.NoError(t, err)

	dir := filepath.Join(configDir, "template", "email")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	custom := "Subject: Custom {app_name}\n---\nCustom body {app_url}\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test.txt"), []byte(custom), 0o600))

	got, err := LoadTemplate("test", configDir)
	require.NoError(t, err)
	assert.Equal(t, "Custom {app_name}", got.Subject)
	assert.NotEqual(t, def.Subject, got.Subject)
}

// TestLoadTemplate_LiveReloadAndReset covers the rest of the two-tier
// lifecycle: edits apply on the next load with no restart, and deleting
// the custom file resets to the embedded default.
func TestLoadTemplate_LiveReloadAndReset(t *testing.T) {
	configDir := t.TempDir()
	def, err := LoadTemplate("backup_failed", configDir)
	require.NoError(t, err)

	first := "Subject: One\n---\nBody {app_name} {app_url} {fqdn}\n"
	_, err = SaveCustomTemplate("backup_failed", configDir, []byte(first))
	require.NoError(t, err)
	assert.True(t, HasCustomTemplate("backup_failed", configDir))

	got, err := LoadTemplate("backup_failed", configDir)
	require.NoError(t, err)
	assert.Equal(t, "One", got.Subject)

	second := "Subject: Two\n---\nBody {app_name} {app_url} {fqdn}\n"
	_, err = SaveCustomTemplate("backup_failed", configDir, []byte(second))
	require.NoError(t, err)

	got, err = LoadTemplate("backup_failed", configDir)
	require.NoError(t, err)
	assert.Equal(t, "Two", got.Subject)

	require.NoError(t, ResetTemplate("backup_failed", configDir))
	assert.False(t, HasCustomTemplate("backup_failed", configDir))

	got, err = LoadTemplate("backup_failed", configDir)
	require.NoError(t, err)
	assert.Equal(t, def.Subject, got.Subject)

	// Resetting an override that is already gone is not an error.
	require.NoError(t, ResetTemplate("backup_failed", configDir))
}

func TestSaveCustomTemplate_RejectsInvalidAndUnknownName(t *testing.T) {
	configDir := t.TempDir()

	_, err := SaveCustomTemplate("not_a_template", configDir, []byte("Subject: x\n---\nbody\n"))
	require.Error(t, err)

	_, err = SaveCustomTemplate("test", configDir, []byte("Subject: \n---\nbody\n"))
	require.Error(t, err)
	assert.Equal(t, "subject cannot be empty", err.Error())
	assert.False(t, HasCustomTemplate("test", configDir))
}

// TestValidateTemplate_Errors covers every blocking check in AI.md
// PART 17 "Template Validation".
func TestValidateTemplate_Errors(t *testing.T) {
	cases := []struct {
		name    string
		tmpl    string
		raw     string
		wantErr string
	}{
		{
			name:    "unknown variable with suggestion",
			tmpl:    "test",
			raw:     "Subject: Hi\n---\nHost is {fqdnn}\n",
			wantErr: "unknown variable: {fqdnn}. Did you mean {fqdn}?",
		},
		{
			name:    "unknown variable without suggestion",
			tmpl:    "test",
			raw:     "Subject: Hi\n---\nValue {completely_unrelated}\n",
			wantErr: "unknown variable: {completely_unrelated}",
		},
		{
			name:    "empty subject",
			tmpl:    "test",
			raw:     "Subject:   \n---\nBody\n",
			wantErr: "subject cannot be empty",
		},
		{
			name:    "empty body",
			tmpl:    "test",
			raw:     "Subject: Hi\n---\n   \n",
			wantErr: "body cannot be empty",
		},
		{
			name:    "invalid syntax",
			tmpl:    "test",
			raw:     "Subject: Hi\n---\nline\nline\n{unclosed\n",
			wantErr: "invalid template syntax at line 5",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateTemplate(tc.tmpl, []byte(tc.raw))
			require.Error(t, err)
			assert.Equal(t, tc.wantErr, err.Error())
		})
	}
}

// TestValidateTemplate_TemplateSpecificVariable proves a template-specific
// variable is accepted in its own template and rejected in another.
func TestValidateTemplate_TemplateSpecificVariable(t *testing.T) {
	raw := "Subject: Backup\n---\nFile {filename} on {fqdn} - {app_name} {app_url}\n"
	_, err := ValidateTemplate("backup_complete", []byte(raw))
	require.NoError(t, err)

	_, err = ValidateTemplate("update_installed", []byte(raw))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "{filename}")
}

// TestValidateTemplate_Warnings covers the non-blocking warnings from
// AI.md PART 17: over-long subject and missing recommended sections.
func TestValidateTemplate_Warnings(t *testing.T) {
	long := strings.Repeat("a", 90)
	warnings, err := ValidateTemplate("test", []byte("Subject: "+long+"\n---\nBody\n"))
	require.NoError(t, err)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "78")

	warnings, err = ValidateTemplate("security_alert", []byte("Subject: Alert\n---\nSomething happened\n"))
	require.NoError(t, err)
	require.NotEmpty(t, warnings)
	joined := strings.Join(warnings, "\n")
	assert.Contains(t, joined, "{app_url}")
	assert.Contains(t, joined, "{fqdn}")
	assert.Contains(t, joined, "{notification_reply_to}")
}

func TestSampleVars(t *testing.T) {
	vars := GlobalVars{AppName: "api", AppURL: "https://example.com", FQDN: "example.com"}
	sample := SampleVars("security_alert", vars)
	assert.Equal(t, "api", sample["app_name"])
	assert.Equal(t, "192.168.1.100", sample["ip"])
	assert.NotEmpty(t, sample["event"])

	// Variables belonging to other templates are not injected.
	_, ok := sample["filename"]
	assert.False(t, ok)
}

// TestSampleVars_RenderLeavesNoTokens proves the preview data set covers
// every variable each built-in template references.
func TestSampleVars_RenderLeavesNoTokens(t *testing.T) {
	vars := GlobalVars{
		AppName:             "api",
		AppURL:              "https://example.com",
		FQDN:                "example.com",
		NotificationReplyTo: "ops@example.com",
	}
	for _, name := range TemplateNames {
		tmpl, err := LoadTemplate(name, t.TempDir())
		require.NoError(t, err, name)
		subject, body := RenderTemplate(tmpl, SampleVars(name, vars))
		assert.NotContains(t, subject, "{", name)
		assert.NotContains(t, body, "{", name)
	}
}

func TestGlobalVars_ToMapUsesFixedClock(t *testing.T) {
	original := nowFunc
	nowFunc = func() time.Time { return time.Date(2031, 4, 5, 6, 7, 8, 0, time.UTC) }
	defer func() { nowFunc = original }()

	m := GlobalVars{AppName: "api"}.toMap()
	assert.Equal(t, "2031", m["year"])
	assert.Contains(t, m["timestamp"], "2031-04-05 06:07:08")
	for _, v := range GlobalVariables {
		_, ok := m[v]
		assert.True(t, ok, v)
	}
}
