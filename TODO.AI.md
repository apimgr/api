## [ ] Fix release.yml BUILD_DATE format
`.github/workflows/release.yml` line 57 stamps `BUILD_DATE` with the custom
format `"%a %b %d, %Y at %H:%M:%S %Z"` instead of ISO 8601 UTC
(`YYYY-MM-DDTHH:MM:SSZ`), inconsistent with `Makefile` line 10 and AI.md
PART 27's CI/CD conventions. Fix: change the `date` invocation to
`date -u +%Y-%m-%dT%H:%M:%SZ`.

## [ ] Fix docker-compose environment list-style and missing x-logging/cache
Found while fixing the volume-mount-path item above (same three files):
`docker/docker-compose.yml`, `docker/docker-compose.dev.yml`, and
`docker/docker-compose.test.yml` all violate `.claude/rules/docker-rules.md`
in two additional ways:
1. `environment:` uses list style (`- MODE=production`) instead of the
   required YAML map style (`MODE: production`) with no `.env` file.
2. None of the three files define the required `x-logging: &default-logging`
   anchor (`max-size: '5m'`, `max-file: '1'`, `driver: json-file`)
   referenced by every service, and `docker-compose.yml`/
   `docker-compose.test.yml` are missing the documented Valkey cache service
   (`api-cache`/`api-cache` container) that the compose-variants table in
   `.claude/rules/docker-rules.md` specifies for those two variants.
Fix: convert `environment:` blocks to map style, add the `x-logging` anchor
to all three files, and add the Valkey cache service to
`docker-compose.yml` and `docker-compose.test.yml`.
