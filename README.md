# Meme Hub Application

A modern, fast, production-ready entertainment and meme sharing portal (inspired by 9GAG) written in Go. The application allows users to discover viral memes, upload funny clips and images, vote posts up or down, discuss them in comment threads, search with Elasticsearch, and authenticate securely using Google OAuth 2.0.

---

## Tech Stack

- **Language & Runtime**: [Go](https://go.dev/) (1.23+)
- **HTTP Server**: Go standard library `net/http` with structured logging (`log/slog`)
- **Database**: [PostgreSQL](https://www.postgresql.org/) using [`github.com/lib/pq`](https://github.com/lib/pq)
- **Database Migrations**: Versioned `.sql` files (`migrations/`) embedded with `embed.FS` and tracked via `schema_migrations`
- **Session Cache**: [Redis](https://redis.io/) using [`github.com/redis/go-redis/v9`](https://github.com/redis/go-redis)
- **Message Broker**: [RabbitMQ](https://www.rabbitmq.com/) via [`github.com/rabbitmq/amqp091-go`](https://github.com/rabbitmq/amqp091-go) for asynchronous moderation, email notification dispatch, and Dead Letter Queue (DLQ)
- **Search Engine**: [Elasticsearch](https://www.elastic.co/elasticsearch) via official [`github.com/elastic/go-elasticsearch/v8`](https://github.com/elastic/go-elasticsearch) for typo-tolerant full-text search across messages, media, and authors
- **Email Delivery & Testing**: Standard Go `net/smtp` client with support for STARTTLS and credentials (compatible with Brevo, MailerSend, Gmail SMTP) + [Mailpit](https://github.com/axllent/mailpit) for zero-configuration local email capture and web UI inspection
- **Authentication**: Google OAuth 2.0 via [`golang.org/x/oauth2`](https://pkg.go.dev/golang.org/x/oauth2)
- **Frontend / Templating**: Server-side rendered HTML using Go's secure `html/template` package (with automatic contextual XSS escaping), Tailwind CSS via CDN, and interactive vanilla JavaScript
- **Containerization**: [Docker & Docker Compose](https://www.docker.com/) for PostgreSQL, Redis, RabbitMQ, Mailpit, Elasticsearch, and App
- **Build & Quality Tooling**: `make`, `golangci-lint`, `goimports`

---

## Project Architecture

The repository strictly follows the [Standard Go Project Layout](https://github.com/golang-standards/project-layout):

```text
meme-hub/
├── cmd/
│   └── server/          # Application entrypoint (main.go)
├── internal/
│   ├── broker/          # RabbitMQ client, topology declaration, DLQ routing, and Publisher interface
│   ├── config/          # Configuration loading and environment parsing
│   ├── handler/         # HTTP handlers, routing, session management, and templates
│   ├── mailer/          # Outgoing email delivery abstractions and SMTP sender
│   ├── moderation/      # Rule-based spam detection and content moderation service
│   ├── repository/      # Storage interface & PostgreSQL database persistence
│   ├── search/          # Elasticsearch search client, index management, and query execution
│   ├── validator/       # Input validation logic (emails, message constraints)
│   └── worker/          # Background consumer workers (moderation and email dispatch)
├── migrations/          # Versioned SQL migrations (up/down) with automatic schema tracking
├── web/
│   └── template/        # HTML templates (index.html)
├── Makefile             # Development automation and quality assurance targets
├── docker-compose.yml   # Multi-container orchestration (Postgres, Redis, RabbitMQ, Mailpit, Elasticsearch, App)
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
| `REDIS_URL` | Redis connection string | `redis://localhost:6379/0` |
| `RABBITMQ_URL` | RabbitMQ connection string | `amqp://guest:guest@localhost:5672/` |
| `SMTP_HOST` | SMTP server host | `localhost` (or `mailpit` in Docker) |
| `SMTP_PORT` | SMTP server port | `1025` (Mailpit) / `587` (Brevo, Gmail) |
| `SMTP_USER` | SMTP username | *(empty for Mailpit)* |
| `SMTP_PASS` | SMTP password / app key | *(empty for Mailpit)* |
| `SMTP_FROM` | Outgoing email sender address | `noreply@guestbook.local` |
| `ADMIN_EMAIL` | Administrator alert recipient | `admin@guestbook.local` |
| `ELASTICSEARCH_URL` | Elasticsearch cluster endpoint | `http://localhost:9200` |
| `GOOGLE_CLIENT_ID` | Google OAuth2 Client ID | `your-google-client-id.apps.googleusercontent.com` |
| `GOOGLE_CLIENT_SECRET` | Google OAuth2 Client Secret | `your-google-client-secret` |
| `GOOGLE_REDIRECT_URL` | Google OAuth2 redirect URI | `http://localhost:8080/callback` |

> [!NOTE]
> Never commit your real `.env` file to version control. The repository includes `.gitignore` patterns to prevent `.env` and `*.env` files from being tracked.

---

### 2. Start Infrastructure Services via Docker Compose

Launch PostgreSQL, Redis, RabbitMQ, Mailpit, and Elasticsearch with a single command:

```bash
docker compose up -d postgres redis rabbitmq mailpit elasticsearch
```

- **PostgreSQL**: Port `5433`
- **Redis**: Port `6379`
- **RabbitMQ**: Port `5672` (AMQP broker) and [http://localhost:15672](http://localhost:15672) (Management Web UI, user: `guest`, pass: `guest`)
- **Mailpit**: Port `1025` (SMTP) and [http://localhost:8025](http://localhost:8025) (Webmail inbox interface to view emails sent by the app)
- **Elasticsearch**: Port `9200` (REST API for full-text search)

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
