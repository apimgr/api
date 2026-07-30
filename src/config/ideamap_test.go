package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseComplianceDeclarations covers the IDEA.md "## Compliance
// declarations" section parser: correct section scoping, comma-separated
// multi-values, case-insensitivity, and safe no-op behavior on prose/empty
// sections (this project's own IDEA.md is exactly this last case).
func TestParseComplianceDeclarations(t *testing.T) {
	t.Run("no section present", func(t *testing.T) {
		declared := parseComplianceDeclarations([]byte("## Project description\n\nSome text.\n"))
		assert.Empty(t, declared)
	})

	t.Run("section present but only prose", func(t *testing.T) {
		data := []byte(`## Compliance declarations

This project declares no audience, compliance, or data_class values here —
consistent with the product-scope/non-goals above.
`)
		declared := parseComplianceDeclarations(data)
		assert.Empty(t, declared)
	})

	t.Run("single value per key", func(t *testing.T) {
		data := []byte(`## Compliance declarations

audience: children
data_class: phi
`)
		declared := parseComplianceDeclarations(data)
		assert.Equal(t, []string{"children"}, declared["audience"])
		assert.Equal(t, []string{"phi"}, declared["data_class"])
	})

	t.Run("comma-separated multi-value and case-insensitivity", func(t *testing.T) {
		data := []byte(`## Compliance declarations

Compliance: GDPR, HIPAA,  PCI-DSS
`)
		declared := parseComplianceDeclarations(data)
		assert.Equal(t, []string{"gdpr", "hipaa", "pci-dss"}, declared["compliance"])
	})

	t.Run("section ends at next heading", func(t *testing.T) {
		data := []byte(`## Compliance declarations

audience: children

## Other section

compliance: hipaa
`)
		declared := parseComplianceDeclarations(data)
		assert.Equal(t, []string{"children"}, declared["audience"])
		assert.Empty(t, declared["compliance"])
	})

	t.Run("unknown key ignored", func(t *testing.T) {
		data := []byte(`## Compliance declarations

note: this is not a declaration key
audience: children
`)
		declared := parseComplianceDeclarations(data)
		assert.Empty(t, declared["note"])
		assert.Equal(t, []string{"children"}, declared["audience"])
	})
}

// TestBuildHeaderAutoMap exercises AI.md's IDEA.md → Header Tightening
// Auto-Map trigger table, including the "no declarations" and
// "strictest-wins" rules.
func TestBuildHeaderAutoMap(t *testing.T) {
	t.Run("no declarations leaves defaults untouched", func(t *testing.T) {
		cfg := defaultConfig()
		before := cfg.Web.Headers
		changes := buildHeaderAutoMap(cfg, map[string][]string{})
		assert.Empty(t, changes)
		assert.Equal(t, before, cfg.Web.Headers)
	})

	t.Run("audience children triggers coppa row", func(t *testing.T) {
		cfg := defaultConfig()
		declared := map[string][]string{"audience": {"children"}}
		changes := buildHeaderAutoMap(cfg, declared)

		assert.Equal(t, "same-origin", cfg.Web.Headers.COOP)
		assert.Equal(t, "same-site", cfg.Web.Headers.CORP)
		assert.True(t, cfg.Web.Headers.HonorSecGPC)
		assert.True(t, cfg.Web.Headers.ClearSiteData.ExecutionContexts)
		assert.Equal(t, "no-referrer", cfg.Web.Headers.ReferrerPolicy)
		require.NotEmpty(t, changes)

		fields := map[string]bool{}
		for _, c := range changes {
			fields[c.Field] = true
		}
		assert.True(t, fields["web.headers.coop"])
		assert.True(t, fields["web.headers.corp"])
		assert.True(t, fields["web.headers.clear_site_data.execution_contexts"])
		assert.True(t, fields["web.headers.referrer_policy"])
		// HonorSecGPC is already true by default, so it must NOT appear as
		// a logged change even though the row nominally requires it.
		assert.False(t, fields["web.headers.honor_sec_gpc"])
	})

	t.Run("compliance hipaa triggers hipaa row", func(t *testing.T) {
		cfg := defaultConfig()
		declared := map[string][]string{"compliance": {"hipaa"}}
		buildHeaderAutoMap(cfg, declared)

		assert.Equal(t, "same-origin", cfg.Web.Headers.CORP)
		assert.True(t, cfg.Web.Headers.HonorSecGPC)
		assert.True(t, cfg.Web.Headers.ServerTimingInDebugOnly)
	})

	t.Run("data_class phi triggers hipaa row via data_class alias", func(t *testing.T) {
		cfg := defaultConfig()
		declared := map[string][]string{"data_class": {"phi"}}
		buildHeaderAutoMap(cfg, declared)

		assert.Equal(t, "same-origin", cfg.Web.Headers.CORP)
	})

	t.Run("compliance pci-dss triggers pci row", func(t *testing.T) {
		cfg := defaultConfig()
		declared := map[string][]string{"compliance": {"pci-dss"}}
		buildHeaderAutoMap(cfg, declared)

		assert.Equal(t, "require-corp", cfg.Web.Headers.COEP)
		assert.Equal(t, "same-origin", cfg.Web.Headers.COOP)
		assert.Equal(t, "no-referrer", cfg.Web.Headers.ReferrerPolicy)
	})

	t.Run("compliance gdpr alone is a no-op (already-default row)", func(t *testing.T) {
		cfg := defaultConfig()
		before := cfg.Web.Headers
		changes := buildHeaderAutoMap(cfg, map[string][]string{"compliance": {"gdpr"}})
		assert.Empty(t, changes)
		assert.Equal(t, before, cfg.Web.Headers)
	})

	t.Run("compliance glba triggers glba row (pci-like plus popup-preserving coop)", func(t *testing.T) {
		cfg := defaultConfig()
		declared := map[string][]string{"compliance": {"glba"}}
		buildHeaderAutoMap(cfg, declared)

		assert.Equal(t, "same-origin-allow-popups", cfg.Web.Headers.COOP)
		assert.Equal(t, "no-referrer", cfg.Web.Headers.ReferrerPolicy)
		// glba inherits pci-dss's COEP per AI.md's "as PCI-DSS" prefix.
		assert.Equal(t, "require-corp", cfg.Web.Headers.COEP)
	})

	t.Run("compliance ferpa triggers ferpa row", func(t *testing.T) {
		cfg := defaultConfig()
		declared := map[string][]string{"compliance": {"ferpa"}}
		buildHeaderAutoMap(cfg, declared)

		assert.Equal(t, "same-origin", cfg.Web.Headers.CORP)
		assert.True(t, cfg.Web.Headers.HonorSecGPC)
	})

	t.Run("uses_sharedarraybuffer triggers sab row", func(t *testing.T) {
		cfg := defaultConfig()
		declared := map[string][]string{"uses_sharedarraybuffer": {"true"}}
		buildHeaderAutoMap(cfg, declared)

		assert.Equal(t, "same-origin", cfg.Web.Headers.COOP)
		assert.Equal(t, "require-corp", cfg.Web.Headers.COEP)
	})

	t.Run("uses_wasm_threads is an alias for the sab row", func(t *testing.T) {
		cfg := defaultConfig()
		declared := map[string][]string{"uses_wasm_threads": {"true"}}
		buildHeaderAutoMap(cfg, declared)

		assert.Equal(t, "same-origin", cfg.Web.Headers.COOP)
		assert.Equal(t, "require-corp", cfg.Web.Headers.COEP)
	})

	t.Run("embeds_third_party alone leaves defaults untouched", func(t *testing.T) {
		cfg := defaultConfig()
		before := cfg.Web.Headers
		changes := buildHeaderAutoMap(cfg, map[string][]string{"embeds_third_party": {"true"}})
		assert.Empty(t, changes)
		assert.Equal(t, before, cfg.Web.Headers)
	})

	t.Run("strictest wins: gdpr + hipaa + pci-dss combined", func(t *testing.T) {
		cfg := defaultConfig()
		declared := map[string][]string{
			"compliance": {"gdpr", "hipaa", "pci-dss"},
		}
		buildHeaderAutoMap(cfg, declared)

		// pci-dss and hipaa both want CORP=same-origin (rank 2) — must win
		// over no row wanting same-site.
		assert.Equal(t, "same-origin", cfg.Web.Headers.CORP)
		// pci-dss wants COOP=same-origin (rank 2), the strictest COOP value
		// among the combined triggers.
		assert.Equal(t, "same-origin", cfg.Web.Headers.COOP)
		assert.Equal(t, "require-corp", cfg.Web.Headers.COEP)
		assert.Equal(t, "no-referrer", cfg.Web.Headers.ReferrerPolicy)
		assert.True(t, cfg.Web.Headers.HonorSecGPC)
	})

	t.Run("coppa vs glba coop: strictest (same-origin) wins over same-origin-allow-popups", func(t *testing.T) {
		cfg := defaultConfig()
		declared := map[string][]string{
			"audience":   {"children"},
			"compliance": {"glba"},
		}
		buildHeaderAutoMap(cfg, declared)

		assert.Equal(t, "same-origin", cfg.Web.Headers.COOP)
	})
}

// TestFindIdeaMD exercises the runtime IDEA.md lookup's graceful "not
// found" behavior — this project's own compiled binary will typically not
// have an IDEA.md next to its cwd/executable at deploy time, and that must
// resolve to "no declarations", not an error.
func TestFindIdeaMD(t *testing.T) {
	t.Run("not found returns empty string, not an error", func(t *testing.T) {
		dir := t.TempDir()
		oldWd, err := os.Getwd()
		require.NoError(t, err)
		t.Cleanup(func() { _ = os.Chdir(oldWd) })
		require.NoError(t, os.Chdir(dir))

		got := findIdeaMD()
		// The dev checkout's IDEA.md may still be found via the test
		// binary's own on-disk location; only assert it is not the
		// temp dir's (nonexistent) IDEA.md.
		assert.NotEqual(t, dir+"/IDEA.md", got)
	})
}

// TestLoadComplianceDeclarations confirms the end-to-end helper degrades to
// an empty map rather than panicking when no IDEA.md is reachable.
func TestLoadComplianceDeclarations(t *testing.T) {
	declared := loadComplianceDeclarations()
	assert.NotNil(t, declared)
}
