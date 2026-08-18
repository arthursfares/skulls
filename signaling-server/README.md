# signaling-server

WebSocket signaling + REST API server for skulls. Handles account
registration/login, room and invite management, and relays WebRTC signaling
(offer/answer/ICE) between connected peers over `/ws`. State is persisted to
a local SQLite database via GORM.

## Layout

- `signaling_server.go` — entrypoint, WebSocket handler, message relay
- `http_api.go` — REST endpoints (register, login, rooms, invites, account)
- `middleware.go` — per-IP rate limiting on auth endpoints
- `rooms.go` — in-memory room/peer state
- `roll.go` — dice-roll notation parsing
- `synthetic.go` — synthetic ("bot") peer support
- `store.go` — SQLite-backed persistent store
- `types_definitions.go` — shared request/response types
- `Dockerfile`, `docker-compose.yml`, `Caddyfile` — deployment

## Running locally

```
go run .
```

Listens on `:8080` and creates `skulls.db` (WAL mode) in the current
directory on first run.

## Running with Docker Compose

Brings up the signaling server behind Caddy, which terminates TLS
automatically via Let's Encrypt.

1. Edit `Caddyfile` and replace `signal.example.com` with your real domain
   (DNS must already point at the host). For local testing without a
   domain/TLS, use the commented `:80` block instead.
2. `docker compose up -d --build`

Services:

- `signaling-server` — the Go binary, internal network only, not published to the host
- `caddy` — reverse proxy, listens on `80`/`443`, forwards to `signaling-server:8080`

Caddy waits for `signaling-server` to report healthy (its Docker `HEALTHCHECK`
does a TCP probe on `8080`) before it starts routing traffic.

## Data persistence

The signaling container stores its SQLite database under
`/signaling-server/data`, backed by the `signaling_data` named volume, so
data survives container restarts and rebuilds.
