# Translating

`src/common/i18n/locales/en.json` is the single source of translation keys and English text. Translators change locale values, never the English source, message keys, or configuration identifiers. Locale files are UTF-8 JSON embedded in both the server and `api-cli` binaries.

## Current locales

The repository currently contains seven locale files: English (`en`), Arabic (`ar`), German (`de`), Spanish (`es`), French (`fr`), Japanese (`ja`), and Chinese (`zh`). The locale registry is the authority for language names, direction, and plural categories. The README, CLI help, and language selector must list the same supported set and count.

## Translating an existing locale

1. Edit only the target locale JSON file for an existing-language update.
2. Preserve the English file's key names, structure, and key order. Translate values only.
3. Use UTF-8, two-space indentation, and valid JSON without comments or trailing commas.
4. Keep every placeholder unchanged, including braces and spelling, such as `{count}`, `{total}`, and `{datetime}`. A placeholder may move within a translated sentence.
5. Provide every CLDR plural category required by the locale.
6. Write complete phrases rather than fragments intended to be joined by code. Use a consistent, neutral register.
7. Keep product and brand names, protocols, technology names, code, file names, configuration keys, URLs, and literal user values such as `dark` and `auto` untranslated.
8. Follow the locale's punctuation, quotation, decimal, and grouping conventions. French text, for example, uses its required spacing before punctuation.

A developer or translator fluent in the language can prepare the translation; others can review and refine it through the normal contribution process. Machine translation is not a substitute for reliable wording in a language the contributor cannot translate competently. Human corrections take precedence over earlier wording.

## Adding a locale

1. Copy `src/common/i18n/locales/en.json` to a file named with the lowercase BCP 47 language code.
2. Translate every key and retain the full structure.
3. Fill the locale `meta` values: language code, English name, native name, direction (`ltr` or `rtl`), and version. Add translator credits only when contributors choose to provide a name or handle.
4. Add the locale to the single language registry with its native name, direction, and CLDR plural categories, and to the configured available-language set.
5. Confirm it appears in the WebUI selector and the `--lang` option of both binaries.
6. Run the i18n locale/key consistency tests and rebuild the server and client binaries so the embedded locale files are updated.
7. Update the README language list and this guide's locale count and credits in the same change.

## Glossary

Use these terms consistently; keep product and protocol names unchanged.

| English term | Guidance |
|--------------|----------|
| API | Keep the protocol name API |
| CLI | Keep the abbreviation CLI |
| token | Translate as a credential token, not as a session or account |
| paste | Use the agreed term for inserting copied content |
| rotate | Replacing a credential while retaining its identity |
| revoke | Invalidating a credential |
| server | The running `api` service |
| client | The companion `api-cli` application |
| theme | The appearance setting: light, dark, or automatic |

If English text is incomplete, ambiguous, or appears to require concatenated fragments, report it for correction instead of working around it in translations.

## Review

Verify key and placeholder parity, plural categories, glossary consistency, and valid JSON. Review translated pages at 320, 768, and 1280 pixels. Check right-to-left layout for RTL languages and long unspaced text for scripts that need explicit font support or wrapping rules. Native-speaker review is welcome but is not a merge requirement.

Translator credits belong here and in the locale's optional `meta.translators` field, with the contributor's chosen name or handle. No translator credits are currently recorded.
