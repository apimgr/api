package server

import (
	"testing"

	"github.com/apimgr/api/src/common/i18n"
	"github.com/stretchr/testify/require"
)

func TestToolMetadataKeysReadTitleAndDescriptionFromLocalizedTemplate(t *testing.T) {
	title, description := toolMetadataKeys("crypto", "hash")
	require.Equal(t, "tool_pages.text.hash.generator.ef470f43f2", title)
	require.Equal(t, "tool_pages.text.generate.cryptographic.hashes.using.various.65b3a4372d", description)
}

func TestEveryToolPageHasLocalizedMetadata(t *testing.T) {
	for _, page := range toolPages() {
		titleKey, descriptionKey := toolMetadataKeys(page.category, page.tool)
		if titleKey == "" || descriptionKey == "" {
			t.Errorf("%s/%s has no localized title or description key", page.category, page.tool)
			continue
		}
		for _, lang := range i18n.SupportedLanguageCodes() {
			if got := i18n.Translate(lang, titleKey); got == "" || got == titleKey {
				t.Errorf("%s/%s title key %q is unavailable in %s", page.category, page.tool, titleKey, lang)
			}
			if got := i18n.Translate(lang, descriptionKey); got == "" || got == descriptionKey {
				t.Errorf("%s/%s description key %q is unavailable in %s", page.category, page.tool, descriptionKey, lang)
			}
		}
	}
}

func TestEveryCategoryHasLocalizedMetadata(t *testing.T) {
	for _, category := range allCategories() {
		for _, lang := range i18n.SupportedLanguageCodes() {
			if got := i18n.Translate(lang, category.NameKey); got == "" || got == category.NameKey {
				t.Errorf("category %s name key %q is unavailable in %s", category.Path, category.NameKey, lang)
			}
			if got := i18n.Translate(lang, category.DescriptionKey); got == "" || got == category.DescriptionKey {
				t.Errorf("category %s description key %q is unavailable in %s", category.Path, category.DescriptionKey, lang)
			}
		}
	}
}

func TestCategoryMetadataUsesTranslationKeys(t *testing.T) {
	title, description := categoryMetadata("en", "text")
	require.Equal(t, "Text Utilities", title)
	require.NotEmpty(t, description)
}
