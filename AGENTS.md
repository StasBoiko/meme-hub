# Go Development Guidelines & Standards

You are an expert Go engineer assisting in this project.
Every time you propose, modify, or generate code, you MUST adhere to the following rules automatically:

## 1. Language & Comments
- STRICT REQUIREMENT: ALL code, variable names, functions, comments, logs, strings or docstrings inside the project MUST be in English only.
- Follow the official Go Doc conventions:
  - Every exported function, type, struct, interface, and constant MUST have a doc comment.
  - The doc comment must start exactly with the name of the exported entity (e.g. `// Config holds...`).
  - Comments must be complete sentences ending with a period.

## 2. Architecture & Project Layout (Standard Go Layout)
- STRICT REQUIREMENT: Adhere strictly to the Standard Go Project Layout. All project components MUST be placed in their designated directories:
  - `cmd/server/`: Application entry point (`main.go`). Responsible only for bootstrapping: loading configuration, initializing dependencies (database, repositories, templates, handlers), starting the HTTP server, and managing graceful shutdown. Do not implement business logic, HTTP routing details, or database queries here.
  - `internal/config/`: Configuration management. Responsible for parsing configuration settings (e.g., environment variables, flags), providing sensible defaults, and exposing structured, typed config objects.
  - `internal/repository/`: Data persistence layer. Defines storage interfaces (e.g., `Repository`) and their concrete database or in-memory implementations. Handles raw SQL, transactions, and persistence concerns.
  - `internal/handler/`: HTTP transport layer. Contains HTTP handlers, routing setup, request payload decoding, input validation orchestration, and response formatting (JSON / HTML). Handlers must depend on repository interfaces, not concrete implementations.
  - `web/`: Web presentation layer and static assets:
    - `web/template/`: HTML template files (e.g., `index.html`).
    - `web/static/`: Static assets such as CSS stylesheets, JavaScript files, and images.
- Root Directory Cleanliness:
  - STRICT PROHIBITION: NEVER place Go application code, handlers, business logic, templates, or static assets directly in the project root directory.
  - The root directory is strictly reserved for repository-level configuration and build manifests only (e.g., `go.mod`, `go.sum`, `Makefile`, `AGENTS.md`, `README.md`, `.env.example`, `.gitignore`, `Dockerfile`).

## 3. Web & Security Guidelines
- Template Security:
  - STRICT REQUIREMENT: Always use Go's standard library `html/template` package (NEVER `text/template`) for parsing and rendering HTML content. `html/template` provides contextual auto-escaping to protect against Cross-Site Scripting (XSS) attacks.
  - Do NOT bypass automatic escaping: never cast untrusted user data to `template.HTML`, `template.JS`, `template.URL`, or similar bypass types unless the data has been rigorously sanitized and audited.
  - Template files must reside under `web/template/` and be loaded via robust template constructors.
- Mandatory `/healthz` Endpoint:
  - The HTTP server MUST expose a `/healthz` endpoint for liveness and readiness probes.
  - Must accept `GET` requests and return HTTP status `200 OK` (e.g., `{"status": "ok"}`) when the service is healthy.
  - Non-GET requests to `/healthz` must be rejected with HTTP `405 Method Not Allowed`.
  - Container orchestrators and monitoring tools rely on this endpoint; it must not be removed or renamed.

## 4. Code Quality & Idiomatic Go
- Adhere to "Effective Go" and "Go Code Review Comments".
- Naming conventions:
  - Use camelCase / PascalCase. No snake_case in Go identifiers.
  - Acronyms must be all caps: `userID`, `apiURL`, `httpServer`, `dsn`, `dbConn`.
  - Receiver names should be short (1-2 letters), e.g., `(s *server)`, never `this` or `self`.
- Error Handling:
  - Never ignore errors. Check `if err != nil` on every call returning an error (e.g., `json.NewEncoder.Encode`).
  - Static error strings (e.g., `errors.New(...)`, `fmt.Errorf(...)`) must start with a lowercase letter and contain NO trailing punctuation.
  - Always pass `context.Context` down to database/network calls (`QueryContext`, `ExecContext`).
- String handling:
  - Always use `utf8.RuneCountInString(...)` instead of `len(...)` when checking text length of human-readable strings.
- Input Validation:
  - Never trust raw user input from HTTP requests (JSON payloads, query params, form data).
  - Always validate incoming fields (boundaries, string lengths via utf8.RuneCountInString, required fields, format correctness) before passing data to the repository or database layer.
  - Return HTTP 400 Bad Request with descriptive error messages when validation fails.
- Idiomatic Go Practices:
  - "Accept interfaces, return structs": constructors should return concrete pointer types, while consumers should accept interfaces.
  - Wrap errors properly using `fmt.Errorf("...: %w", err)` to preserve the error chain for `errors.Is` and `errors.As`.
  - Use structured logging via the standard `log/slog` package instead of raw `log.Printf`.
  - Avoid magic numbers and hardcoded strings; define typed constants for pagination limits, status codes, and timeouts.
  - Concurrency & Goroutines: ensure concurrent safety using `sync.RWMutex` for shared in-memory state; protect against goroutine leaks by propagating `context.Context`.    

## 5. Testing Standards
- Whenever a new handler, service method, or business logic function is added or modified, provide or update corresponding unit tests in a `*_test.go` file.
- Use Table-Driven Tests pattern.
- Rely on standard `testing` and `net/http/httptest` packages.
- Isolate dependencies using interfaces (Repository/MockRepository); do NOT connect to live external databases in unit tests.

## 6. Verification Workflow
- After proposing code changes, ensure they can pass:
  1. `go fmt ./...` / `goimports`
  2. `golangci-lint run ./...`
  3. `go test -v -race ./...`
- Do not make unnecessary large refactorings outside the scope of the user request.
