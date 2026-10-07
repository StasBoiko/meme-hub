# Guestbook Application

A clean, modern, production-ready guestbook web application written in Go. The application allows visitors to browse guest entries, authenticate securely using Google OAuth 2.0, and sign the guestbook with personalized messages.

---

## Tech Stack

- **Language & Runtime**: [Go](https://go.dev/) (1.23+)
- **HTTP Server**: Go standard library `net/http` with structured logging (`log/slog`)
- **Database**: [PostgreSQL](https://www.postgresql.org/) using [`github.com/lib/pq`](https://github.com/lib/pq)
- **Authentication**: Google OAuth 2.0 via [`golang.org/x/oauth2`](https://pkg.go.dev/golang.org/x/oauth2)
- **Frontend / Templating**: Server-side rendered HTML using Go's secure `html/template` package (with automatic contextual XSS escaping) and vanilla modern CSS
- **Containerization**: [Docker](https://www.docker.com/) for isolated local PostgreSQL service
- **Build & Quality Tooling**: `make`, `golangci-lint`, `goimports`

---

## Project Architecture

The repository strictly follows the [Standard Go Project Layout](https://github.com/golang-standards/project-layout):

```text
guestbook-app/
├── cmd/
│   └── server/          # Application entrypoint (main.go)
├── internal/
│   ├── config/          # Configuration loading and environment parsing
│   ├── handler/         # HTTP handlers, routing, session management, and templates
│   ├── repository/      # Storage interface & PostgreSQL database persistence
│   └── validator/       # Input validation logic (emails, message constraints)
├── web/
│   └── template/        # HTML templates (index.html)
├── Makefile             # Development automation and quality assurance targets
├── .env.example         # Template for environment configuration
└── .gitignore           # Git ignore rules for binaries, credentials, and OS/IDE files
```

---

## Prerequisites

Before running the application locally, ensure you have installed:

- **Go** (version 1.23 or newer)
- **Docker** (for running the PostgreSQL database container)
- **Make** (standard on Linux/macOS)
- *(Optional)* Google OAuth 2.0 Client credentials (if you wish to test sign-in flow)

---

## Quick Start Guide

### 1. Configure Environment Variables

Create your local `.env` configuration file from the provided template:

```bash
cp .env.example .env
```

Review and adjust variables in `.env` as needed:

| Variable | Description | Default / Example |
| :--- | :--- | :--- |
| `PORT` | HTTP server port | `8080` |
| `DATABASE_URL` | PostgreSQL connection string | `postgres://postgres:secret@localhost:5433/guestbook_db?sslmode=disable` |
| `GOOGLE_CLIENT_ID` | Google OAuth2 Client ID | `your-google-client-id.apps.googleusercontent.com` |
| `GOOGLE_CLIENT_SECRET` | Google OAuth2 Client Secret | `your-google-client-secret` |
| `GOOGLE_REDIRECT_URL` | Google OAuth2 redirect URI | `http://localhost:8080/callback` |

> [!NOTE]
> Never commit your real `.env` file to version control. The repository includes `.gitignore` patterns to prevent `.env` and `*.env` files from being tracked.

---

### 2. Start PostgreSQL via Docker

You can launch a local PostgreSQL container mapped to port `5433`:

```bash
docker run --name guestbook-postgres \
  -e POSTGRES_USER=postgres \
  -e POSTGRES_PASSWORD=secret \
  -e POSTGRES_DB=guestbook_db \
  -p 5433:5432 \
  -d postgres
```

If the container has already been created, start it anytime using:

```bash
docker start guestbook-postgres
```

> [!TIP]
> Database schema migrations are executed automatically on application startup, so no manual SQL initialization is required.

---

### 3. Run the Application

Start the web server locally using `make run`:

```bash
make run
```

Alternatively, you can run:

```bash
go run ./cmd/server
```

Once running, navigate to [http://localhost:8080](http://localhost:8080) in your web browser.

---

## HTTP Endpoints

| Method | Endpoint | Description |
| :--- | :--- | :--- |
| `GET` | `/` | Guestbook web interface (displays messages and submission form) |
| `GET` | `/healthz` | Health check endpoint for liveness and database connectivity probes |
| `GET` | `/login` | Initiates Google OAuth 2.0 authentication flow |
| `GET` | `/callback` | Handles OAuth 2.0 callback from Google and establishes session |
| `POST` | `/logout` | Signs out the current user and clears session cookie |
| `POST` | `/messages` | Submits a new guestbook entry (requires authentication) |
| `GET` | `/api/messages` | Returns the latest guestbook messages formatted as JSON |
| `GET` | `/api/me` | Returns the authenticated user profile as JSON |

---

## Verification & Quality Assurance (`make check`)

The repository includes a comprehensive `Makefile` to verify code formatting, module dependencies, static analysis, and test suites.

Run the full verification pipeline before committing or pushing changes:

```bash
make check
```

The `make check` target sequentially executes:

1. **`make dep`** — Runs `go mod tidy` to clean up and verify dependencies in `go.mod` and `go.sum`.
2. **`make fmt`** — Formats source code using `go fmt` and `goimports`.
3. **`make lint`** — Performs strict static analysis using `golangci-lint run ./...`.
4. **`make test`** — Executes all unit and handler tests with the Go race detector enabled (`go test -v -race ./...`).

### Additional Make Targets

- **`make build`** — Compiles the binary into `bin/server`.
- **`make run`** — Starts the application server directly (`go run ./cmd/server`).
- **`make pre-commit`** — Alias for `make check`.
- **`make help`** — Displays an overview of all available targets.

---

## Author

- **Stanislav Boiko** ([@StasBoiko](https://github.com/StasBoiko))
