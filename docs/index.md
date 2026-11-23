# API

API is a self-hosted utility service with a server-rendered web interface, versioned REST API, GraphQL, and a companion `api-cli` client.

## Features

- Text, cryptography, conversion, date and time, generators, validators, math, and developer utilities
- Parsing, networking, geolocation, weather, image, language, research, Docker, testing, and entertainment tools
- Server-rendered forms that work without JavaScript
- OpenAPI and GraphQL documentation interfaces
- Seven interface locales: English, Arabic, German, Spanish, French, Japanese, and Chinese
- Anonymous credential-based resource ownership without user accounts or login

## Quick start

From a repository clone, start the production Docker Compose stack:

```bash
docker compose -f docker/docker-compose.yml up -d
```

The included Compose configuration publishes the web interface on `http://172.17.0.1:64580/`. For other installation methods, see [Installation](installation.md).

## Documentation

- [Installation](installation.md)
- [Configuration](configuration.md)
- [REST API, OpenAPI, and GraphQL](api.md)
- [CLI Reference](cli.md)
- [Security](security.md)
- [Integrations](integrations.md)
- [Development](development.md)
- [Translating](translating.md)

## Project links

- [Repository](https://github.com/apimgr/api)
- [Issue tracker](https://github.com/apimgr/api/issues)
- [Interactive OpenAPI UI](/server/docs/swagger)
- [Interactive GraphQL UI](/server/docs/graphql)

## License

MIT. See the repository’s [LICENSE.md](https://github.com/apimgr/api/blob/main/LICENSE.md).
