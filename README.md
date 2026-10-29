# mpaas

mpaas is a self-hosted mini platform-as-a-service that automatically builds and deploys applications from a public GitHub URL. The whole system comes up with a single `docker compose up` on a clean machine that has Docker installed and running. It works by building images with Railpack, deploying the built images in Docker containers, and routing traffic to the container via Caddy; all on a single network.

## Contents
- [Prerequisites](#prerequisites)
- [Quickstart](#quickstart)
- [Architecture](#architecture)
- [API reference](#api-reference)
- [Repo layout](#repo-layout)
- [App requirements](#app-requirements)
- [Environment variables](#environment-variables)
- [Root directory](#root-directory)
- [Configuration](#configuration)
- [Behavior and limits](#behavior-and-limits)
- [Security](#security)
- [Common issues and fixes](#common-issues-and-fixes)
- [Development and testing](#development-and-testing)

## Prerequisites
- Docker (Engine or Desktop) with Compose

## Quickstart
```bash
docker compose up -d --build
```
This starts the system. The client is accessible at `http://localhost:3000`, and the deployments are reachable at `http://<owner_name>-<repo_name>[-<root-dir>].localhost`.

## Architecture
```
Browser
 │ http://localhost:3000
 ▼
┌───────────────────────────────────┐
│ client: SvelteKit static build,   │
│ served by Caddy                   │
└─────────────────┬─────────────────┘
 │ /api/* proxied (SSE logs stream back)
 ▼
┌───────────────────────────────────┐
│ api: Go (net/http) + SQLite :8000 │
└─────────────────┬─────────────────┘
 │ per deployment, in a goroutine:
 │ clone → build → run → health check → route
 │
 ├── build ──► railpack prepare + docker buildx
 │             (BuildKit is built into the Docker daemon)
 ├── run ────► Docker daemon (/var/run/docker.sock)
 │             ──► app container on the mpaas network (:$PORT)
 └── route ──► caddy admin API (caddy:2019)

Browser ── http://<owner>-<repo>.localhost ──► caddy :80 ──► <container>:<port>
```

### Client
The client is built with SvelteKit, and the static build is served by Caddy, proxying `/api` to the API.

### API
The API is built with Go. It uses an SQLite database as the source of truth for deployments. It builds Docker images with Railpack + buildx. Each deployment clones the repository's default branch into a temporary directory inside the API container (under `/tmp`). The build runs from there (or from `root_dir` within it), and the directory is deleted when the pipeline finishes, whether it succeeds or fails. Nothing is reused between deployments. Every deploy clones the repo afresh.

### Caddy
Caddy is used for creating dynamic routes for deployments through its API on the `mpaas` network created by compose.

## API reference
```bash
GET    /api/health                          # 200 {"status":"ready"} | 503 {"status":"error","message":"..."}
POST   /api/deployments                     # create, JSON body required: 202 | 400 | 415 | 500
GET    /api/deployments                     # list: 200 | 500
GET    /api/deployments/{id}                # get one: 200 | 404 | 500
POST   /api/deployments/{id}/stop           # stop: 200 | 404 | 500
DELETE /api/deployments/{id}                # delete: 200 | 404 | 500
GET    /api/deployments/{id}/logs/stream    # live logs (SSE): 200 | 404 | 500
```

Create request (`Content-Type: application/json`; only `github_url` is required):

```json
{
  "github_url": "https://github.com/owner/repo",
  "port": 8000,
  "env": { "NAME": "value" },
  "root_dir": "apps/web"
}
```

- `port`: the port the app listens on; injected as `$PORT`. Default `8080`.
- `env`: variables available at build time and runtime. Keys: letters, digits, `_`, not starting with a digit; `PORT` is reserved.
- `root_dir`: relative subfolder to build from.

Responses use an envelope:

```json
{ "success": true, "message": "deployment created successfully", "data": { } }
```

Errors:
- `400`: invalid body, GitHub URL, port, `env`, or `root_dir` (the message says which).
- `415`: body is not JSON.
- `404`: unknown deployment.
- `500`: unexpected failure.

Deployment object:

```json
{
  "id": "01M5...",
  "status": "pending | building | deploying | running | failed",
  "github_url": "https://github.com/owner/repo",
  "image_name": "owner-repo",
  "container_name": "owner-repo-1793010485960067639",
  "container_port": 8080,
  "caddy_route": "owner-repo.localhost",
  "root_dir": "",
  "created_at": "2026-10-26T10:28:03Z",
  "updated_at": "2026-10-26T10:28:16Z"
}
```

`env` values are never returned. The failure reason appears in the logs, not on the deployment.

Log stream events:

```
id: <log id>
event: log
data: {"id":"...","deployment_id":"...","message":"...","created_at":"..."}
```

Stored logs are replayed on connect, then live events follow. Reconnects resume via `Last-Event-ID`. Listen for the named event `log`.

## Repo layout
```text
├── docker-compose.yml      stack: client, api, caddy
├── api/
│   ├── Dockerfile
│   ├── config/Caddyfile    ingress Caddy config
│   ├── cmd/                entry point: config, wiring, HTTP server
│   └── internal/
│       ├── caddy/          Caddy admin API client (routes)
│       ├── db/             SQLite via GORM
│       ├── docker/         Docker SDK wrapper (run, stop, logs, network)
│       ├── handler/        HTTP handlers, router, CORS
│       ├── models/         Deployment, LogEntry
│       ├── service/        deploy pipeline (clone, build, run, health check, route)
│       └── stream/         in-memory pub/sub for live logs
└── client/
    ├── Dockerfile
    ├── config/Caddyfile    serves the build, proxies /api
    └── src/                SvelteKit app
```

## App requirements
- Deployed apps must listen on `$PORT`. The default is `8080`.
- Deployed apps must bind all interfaces.
- Deployed apps must serve plain HTTP.

## Environment variables
Environment variables are available at build time (via Railpack secrets) and at runtime. `PORT` is reserved.

## Root directory
For apps in a subfolder, their root directory can be set: `root_dir`(e.g. `app`). Workspace dependencies outside it aren't available.

## Configuration
- `PORT` and `CADDY_ADMIN_URL` are set in the compose file’s `environment` block for `api`.
- CORS in the API is hardcoded to `http://localhost:3000` and `http://127.0.0.1:3000`.

## Behavior and limits
- Deploying a repo again overrides its current route.
- Only the default branch is deployed.
- Apps cannot be restarted (redeploy instead).
- Sanitized hostnames can collide (e.g. `my_app` / `my-app`).
  
## Security
- As this is a local, single-user tool, there is no user authentication or rate-limiting.
- The API and Caddy ingress bind to loopback and serve plain HTTP.
- Mounting the Docker socket gives the API root-equivalent access to the host. mpaas should be run on only trusted machines.
- Deploy only trusted public repos.
- env values are stored in plaintext in SQLite.
- `railpack prepare --env` briefly exposes env values in process list.

## Common issues and fixes
- Docker not running - Start the Docker Engine or Desktop.
- Port `8000/3000/80` in use - Find what's using the port (`ss -ltnp`), and end the process if need be.
- App exits immediately after build - Railpack couldn’t determine a start command: add one, or check the logs for Railpack’s hint.
- Build values missing - Environment variables not set.

## Development and testing
Test the API with:
```bash
cd api
go test ./...
```
Start the client with:
```bash
cd client
npm install
npm run dev
```
