# API Toolkit Documentation

Welcome to the **API Toolkit** documentation. This is a versatile REST API service providing multiple utility endpoints for common development tasks.

## What is API Toolkit?

API Toolkit is a universal API platform that offers various utility services through a unified interface:

- **Text Utilities** - String manipulation, encoding, hashing, and formatting
- **Cryptographic Operations** - Password hashing, encryption, token generation
- **Date/Time Utilities** - Timezone conversions, timestamp calculations, formatting
- **Network Utilities** - IP information, header inspection, GeoIP lookups

## Key Features

### Multiple API Interfaces

- **REST API** - Standard HTTP/JSON endpoints at `/api/v1/`
- **Swagger/OpenAPI** - Interactive API documentation at `/openapi`
- **GraphQL** - Flexible query interface at `/graphql`

### Modern Web Interface

- Responsive web UI with dark/light/auto theme support
- Admin panel for server configuration
- Real-time API testing and exploration

### Production Ready

- SSL/TLS support with Let's Encrypt integration
- Rate limiting and security headers
- Comprehensive logging and monitoring
- Automated backup and restore
- Docker containerization
- Systemd service integration

## Quick Start

```bash
# Run with Docker
docker run -p 64580:80 ghcr.io/apimgr/api:latest

# Or install as system service
./api --service install
./api --service start
```

Visit `http://localhost:64580` to access the web interface.

## Documentation Structure

- **[Installation](installation.md)** - How to install and deploy
- **[Configuration](configuration.md)** - Server configuration options
- **[API Reference](api.md)** - Complete API endpoint documentation
- **[CLI Reference](cli.md)** - Command-line interface usage
- **[Admin Panel](admin.md)** - Web admin interface guide
- **[Development](development.md)** - Contributing and development guide

## Use Cases

- **Development Workflows** - Utility endpoints for development and testing
- **CI/CD Pipelines** - Automated text processing and crypto operations
- **Infrastructure Automation** - Date/time calculations and network utilities
- **Microservice Architecture** - Shared utility services across applications

## License

This project is licensed under the MIT License. See [LICENSE.md](https://github.com/apimgr/api/blob/main/LICENSE.md) for details.

## Support

- **Issues**: [GitHub Issues](https://github.com/apimgr/api/issues)
- **Source Code**: [GitHub Repository](https://github.com/apimgr/api)
- **Documentation**: [ReadTheDocs](https://apimgr-api.readthedocs.io)
