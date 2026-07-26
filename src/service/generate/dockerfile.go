package generate

import (
	"fmt"
	"strings"
)

// dockerfileTemplates holds a curated, minimal multi-stage Dockerfile per
// language. This is a static-template scope (?lang=), distinct from the
// structured-JSON-config Dockerfile builder in src/service/docker, which
// covers a different route (/docker/dockerfile-generate) and is not
// duplicated here.
var dockerfileTemplates = map[string]string{
	"go": `FROM golang:1.23-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /app/bin/app ./...

FROM alpine:latest
RUN apk add --no-cache ca-certificates
COPY --from=build /app/bin/app /app/app
ENTRYPOINT ["/app/app"]
`,
	"node": `FROM node:22-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM node:22-alpine
WORKDIR /app
COPY --from=build /app ./
CMD ["node", "index.js"]
`,
	"python": `FROM python:3.12-slim AS build
WORKDIR /app
COPY requirements.txt ./
RUN pip install --no-cache-dir --prefix=/install -r requirements.txt

FROM python:3.12-slim
WORKDIR /app
COPY --from=build /install /usr/local
COPY . .
CMD ["python", "main.py"]
`,
	"rust": `FROM rust:1.82 AS build
WORKDIR /app
COPY Cargo.toml Cargo.lock ./
COPY src ./src
RUN cargo build --release

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=build /app/target/release/app /app/app
ENTRYPOINT ["/app/app"]
`,
	"generic": `FROM alpine:latest
WORKDIR /app
COPY . .
CMD ["/app/entrypoint.sh"]
`,
}

// Dockerfile returns a minimal idiomatic multi-stage Dockerfile for the
// given language, from a curated static set (go, node, python, rust,
// generic).
func (s *Service) Dockerfile(lang string) (string, error) {
	key := strings.ToLower(strings.TrimSpace(lang))
	if key == "" {
		key = "generic"
	}
	tmpl, ok := dockerfileTemplates[key]
	if !ok {
		return "", fmt.Errorf("unsupported language %q: supported languages are go, node, python, rust, generic", lang)
	}
	return tmpl, nil
}
