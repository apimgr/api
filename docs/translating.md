# Translating

The application currently ships seven locale files: English (en), Arabic (ar), German (de), Spanish (es), French (fr), Japanese (ja), and Chinese (zh). Locale JSON files live in `src/common/i18n/locales/`.

## Updating a translation

English (en.json) is the source file. When a key is added, changed, or removed in English, make the corresponding change in each supported locale in the same update.

- Keep the same JSON structure, key names, and key order as English.
- Use UTF-8 JSON with two-space indentation and no comments or trailing commas.
- Translate string values only. Keep product names, code, URLs, file names, configuration keys, and protocol names unchanged.
- Preserve every placeholder exactly, including its name and braces, such as `{count}` or `{datetime}`.
- Keep complete phrases instead of fragments that are joined in code.
- Include all CLDR plural forms required by the language.
- Follow the language’s normal punctuation, quotation, number, and date conventions.

## Adding a locale

Add a locale only when its translations can be maintained. Copy the key structure from `en.json`, translate the values, and register the locale in the i18n language registry. Set the correct native display name and text direction. Review the interface at 320, 768, and 1280 pixels; check right-to-left layout where applicable and verify that long text wraps without clipping.

## Glossary

Reuse these terms consistently. Product and protocol names remain unchanged.

| English term | Guidance |
|--------------|----------|
| API | Keep the protocol name API |
| CLI | Keep the abbreviation CLI |
| token | Translate as a credential token; do not confuse it with a session or account |
| rotate | Use the term for replacing a credential while retaining its identity |
| revoke | Use the term for invalidating a credential |
| server | The running `api` service |
| client | The companion `api-cli` application |
| theme | The appearance setting: light, dark, or automatic |

## Review and credits

Review that keys and placeholders match English, required plural forms are present, and the interface remains readable in the target language. Translator credits may be added to a locale’s optional `meta.translators` field and documented here when contributors provide a name or handle. No translator credits are currently recorded.
