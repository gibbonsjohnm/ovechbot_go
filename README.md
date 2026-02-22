# Ovechbot Go

Distributed Go application using a **Producer-Consumer (Ingestor/Announcer)** pattern to track Alex Ovechkin's NHL goal count and emit notifications.

## Architecture

- **Ingestor**: Polls the [NHL API](https://api-web.nhle.com/v1/player/8471214/landing) for Ovechkin's career regular-season goals. When the count increases, it emits a "Goal Event" to a **Redis Stream** (`ovechkin:goals`).
- **Announcer**: Subscribes to the stream via a Redis **Consumer Group** (`announcers`). When a goal event is received, it posts a **Discord** message (rich embed with emojis and Ovechkin image) to a channel and runs a **Discord bot** with slash commands for chatters.

## Layout

- **Go Workspace** (`go.work`): manages the `ingestor` and `announcer` modules.
- **ingestor/** and **announcer/**: each has `cmd/`, `internal/`, `go.mod`, and a multi-stage **Dockerfile**.

## Requirements

- Go 1.21+ (for local build and `slog`)
- Docker and Docker Compose

## Run with Docker Compose

```bash
docker compose up --build
```

- **Redis Stack**: `localhost:6379` (Redis), `localhost:8001` (Redis Insight UI for observability).
- **Ingestor**: polls every 60s by default; set `POLL_INTERVAL` to change (e.g. `30s`).
- **Announcer**: reads from the stream; if Discord is configured, posts goal announcements to a channel and responds to slash commands.

### Discord (goal announcements + bot commands)

To post goal announcements to a Discord server and enable slash commands, set these env vars for the **announcer** (e.g. in `docker-compose` or a `.env` file; do not commit tokens):

| Env | Required | Description |
|-----|----------|-------------|
| `DISCORD_BOT_TOKEN` | Yes (for Discord) | Bot token from [Discord Developer Portal](https://discord.com/developers/applications) → your app → Bot → Token |
| `DISCORD_ANNOUNCE_CHANNEL_ID` | Yes (for announcements) | Channel ID where goal alerts are posted (right‑click channel → Copy ID; enable Developer Mode in Discord) |
| `DISCORD_GUILD_ID` | No | Server (guild) ID for registering slash commands in one server; omit to register commands globally |
| `DISCORD_OVECHKIN_IMAGE_URL` | No | Image URL for the goal embed thumbnail; default is NHL headshot |

**Slash commands** (chatters can use these in any channel the bot can see):

- **`/goals`** – Alex Ovechkin’s career goal total (regular season), live from the NHL API.
- **`/lastgoal`** – Date, opponent, and opposing goalie for his most recent goal (from last 5 games).
- **`/ping`** – Check if the bot is online.

The bot’s status shows **Watching HOME vs AWAY** (e.g. `Watching WSH vs PHI`) when a live Capitals game is on (polled from NHL schedule); otherwise **Nothing :(**.

Goal announcements are **rich embeds**: 🚨 GOAL! 🚨 title, Ovechkin image thumbnail, and career goal count.

### Inject a test goal (see the Announcer react)

You can push a fake “goal increased” event into the stream and watch the Announcer log it. The **Ingestor** only writes when the real NHL API shows a higher count; it does not react to manually added messages.

```bash
./scripts/inject-test-goal.sh
```

Then check the Announcer container logs; you should see a structured log line like `"goal notification"` with `goals: 999`. To inject manually with redis-cli:

```bash
docker compose exec redis redis-cli XADD ovechkin:goals '*' payload '{"player_id":8471214,"goals":999,"recorded_at":"2025-02-22T12:00:00Z"}' goals 999
```

## Run locally (without Docker)

1. Start Redis (e.g. `docker run -d -p 6379:6379 redis:7-alpine` or use Redis Stack).
2. From repo root:

```bash
# Ingestor
go run ./ingestor/cmd/ingestor

# Announcer (separate terminal)
go run ./announcer/cmd/announcer
```

Env (optional): `REDIS_ADDR` (default `redis:6379`; use `localhost:6379` for local Redis), `POLL_INTERVAL` (ingestor only, default `60s`). For Discord: `DISCORD_BOT_TOKEN`, `DISCORD_ANNOUNCE_CHANNEL_ID`, `DISCORD_GUILD_ID`, `DISCORD_OVECHKIN_IMAGE_URL` (see table above).

## Graceful shutdown

Both services handle **SIGTERM** and **SIGINT** and exit cleanly (context cancellation and structured shutdown logs).

## Logging

Structured JSON logging via Go’s **slog** (`log/slog`) to stdout.

## Testing

Requires Go 1.21+. Unit tests use `net/http/httptest` for the NHL client and [miniredis](https://github.com/alicebob/miniredis) for Redis stream behavior.

```bash
# From repo root (runs both modules)
go test ./ingestor/internal/... ./announcer/internal/... -v

# Per module
go test ./ingestor/internal/... -v
go test ./announcer/internal/... -v
```
