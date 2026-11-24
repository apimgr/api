# Vendored browser assets

Swagger UI and GraphiQL are the only pages that use vendored JavaScript libraries. Their assets are pinned, served from `src/server/static/vendor/`, and loaded only by their matching documentation page. Do not edit minified vendor files by hand. Update the recorded package versions, licenses, checksums, and this document together when refreshing them.

| Tool | Package | Version | License files |
|---|---|---:|---|
| Swagger UI | `swagger-ui-dist` | 5.36.0 | `swagger-ui/LICENSE`, `swagger-ui/NOTICE` |
| GraphiQL | `graphiql` | 3.9.0 | `graphiql/LICENSE` |
| GraphiQL runtime | `react` | 18.3.1 | `graphiql/LICENSE.react` |
| GraphiQL runtime | `react-dom` | 18.3.1 | `graphiql/LICENSE.react-dom` |

The exact package versions are also recorded in each vendor directory's `VERSIONS.txt`. Minified files are copied from the published package distributions and served locally; the page startup code remains in `src/server/static/js/docs-swagger.js` and `docs-graphql.js`.

SHA-256 checksums for vendor files can be regenerated with:

```sh
sha256sum src/server/static/vendor/swagger-ui/* src/server/static/vendor/graphiql/*
```
