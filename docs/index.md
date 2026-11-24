# API

API is a self-hosted utility service with a server-rendered web interface, versioned REST API, GraphQL, and a companion `api-cli` client.

## Features

- Text, cryptography, conversion, date and time, generators, validators, math, and developer utilities
- Parsing, networking, geolocation, weather, image, language, research, Docker, testing, and entertainment tools
- Server-rendered forms that work without JavaScript
- OpenAPI and GraphQL documentation interfaces
- Eight interface locales: English, Spanish, French, German, Italian, Arabic, Chinese, and Japanese
- Anonymous credential-based resource ownership without user accounts or login
- Browser owner-token management at `/server/preferences/tokens`; its forms work without JavaScript. See [Security](security.md#owner-tokens).

## Quick start

From a repository clone, copy the production Compose file to a deployment directory so its relative volume mounts stay outside the source checkout:

```bash
mkdir -p "$HOME/api-deployment"
cp docker/docker-compose.yml "$HOME/api-deployment/compose.yml"
cd "$HOME/api-deployment"
docker compose -f compose.yml up -d
```

The included Compose configuration publishes the web interface on `http://172.17.0.1:64580/` and keeps runtime data under `$HOME/api-deployment/volumes/`. For other installation methods, see [Installation](installation.md).

## Documentation

- [Installation](installation.md)
- [Configuration](configuration.md)
- [REST API, OpenAPI, and GraphQL](api.md)
- [CLI Reference](cli.md)
- [Security](security.md)
- [Integrations](integrations.md)
- [Development](development.md)
- [Translating](translating.md)
- [Vendored browser assets](vendor-assets.md)

## Project links

- [Repository](https://github.com/apimgr/api)
- [Issue tracker](https://github.com/apimgr/api/issues)
- OpenAPI UI on a running instance: `/server/docs/swagger`
- GraphQL UI on a running instance: `/server/docs/graphql`

## License

MIT. See the repository’s [LICENSE.md](https://github.com/apimgr/api/blob/main/LICENSE.md).
