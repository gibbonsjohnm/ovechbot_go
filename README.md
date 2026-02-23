# Ovechbot Go

Distributed Go application using a **Producer-Consumer (Ingestor/Announcer)** pattern to track Alex Ovechkin's NHL goal count and emit notifications.

## Architecture

- **Ingestor**: Polls the [NHL API](https://api-web.nhle.com/v1/player/8471214/landing) for Ovechkin's career regular-season goals. When the count increases, it emits a "Goal Event" to a **Redis Stream** (`ovechkin:goals`).
- **Announcer**: Subscribes to the stream via a Redis **Consumer Group** (`announcers`). When a goal event is received, it posts a **Discord** message (rich embed) to a channel and runs a **Discord bot** with slash commands. It also consumes **pre-game reminders** from `ovechkin:reminders` and **post-game evaluations** from `ovechkin:post_game`, posting both to Discord.
- **Collector**: Periodically fetches Ovechkin’s **game log** (per-game goals, opponent, home/away) and **standings** (team goals-against) from the free NHL API and stores them in Redis (`ovechkin:game_log`, `standings:now`) for the predictor.
- **Predictor**: Every 10 minutes, fetches the next Capitals game. It computes a **scoring probability** (heuristic: career GPG, opponent strength, home/away, recent form; **no ML**) and writes it to `ovechkin:next_prediction` so **`/nextgame`** can show it. If **ODDS_API_KEY** is set, it also fetches **Ovechkin anytime goal scorer** odds from [The Odds API](https://the-odds-api.com) and stores them with the prediction. Writes a snapshot per game to `ovechkin:prediction_snapshot:{game_id}` (7-day TTL) for the evaluator. When that game is **~1 hour** away (55–65 min), it publishes a reminder to `ovechkin:reminders` (announcer posts to Discord) and marks the game sent. Example: “Caps game in ~1 hour · vs **PHI** (HOME). Ovi scoring chance: **42%** · Anytime goal: **+140**”.

- **Evaluator**: Runs every 30 minutes. After each completed Capitals game it fetches Ovechkin’s line (goals, assists, points, TOI, shifts, SOG) from the NHL boxscore, compares to our prediction snapshot, and **sends a Discord message** with a post-game summary and whether the prediction was a **Hit** (e.g. predicted ≥50% and he scored, or &lt;50% and he didn’t) or **Miss**. Does not feed into future predictions.

## Layout

- **Go Workspace** (`go.work`): `ingestor`, `announcer`, `collector`, `predictor`, `evaluator`.
- Each module has `cmd/`, `internal/`, `go.mod`, and a **Dockerfile**.

## Requirements

- Go 1.21+ (for local build and `slog`)
- Docker and Docker Compose

## Run with Docker Compose

```bash
docker compose up --build
```

- **Redis Stack**: `localhost:6380`, `localhost:8002` (Redis Insight).
- **Ingestor**: polls every 60s; `POLL_INTERVAL` to change.
- **Collector**: refreshes game log and standings every 6h; `COLLECTOR_INTERVAL` to change.
- **Predictor**: every 10 min, computes Ovi scoring % for the next game and writes to `ovechkin:next_prediction` (for `/nextgame`); when that game is in 55–65 min, also publishes to `ovechkin:reminders`.
- **Announcer**: consumes `ovechkin:goals` and `ovechkin:reminders`; posts goal announcements and pre-game reminders to Discord and runs slash commands.
- **Evaluator**: every 30 min, checks for the latest completed Caps game. If not yet reported, fetches boxscore (Ovi’s stats) and our prediction snapshot, then publishes one post-game summary to the Redis stream `ovechkin:post_game`. The **announcer** consumes that stream and posts the summary to Discord (same channel as goals/reminders), so no separate Discord config is needed for the evaluator.

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
- **`/lastgoal`** – Date, opponent, and opposing goalie for his most recent goal. When the last goal we announced is still the current total, the reply is served from the **stream cache** (same data we posted); otherwise it fetches from the NHL API (last 5 games + boxscore).
- **`/nextgame`** – Next (or current) Washington Capitals game: opponent, venue, and start time (Eastern). If the predictor has run, also shows **Ovi scoring chance: X%** and, when odds are available, **Anytime goal: +XXX** (market line from The Odds API).
- **`/ping`** – Check if the bot is online.

**Possible future commands:** `/gap` (goals behind Gretzky’s 894), `/milestone` (next round number and how many away), `/last5` (goals in each of last 5 games from landing API).

The bot’s status shows **Watching HOME vs AWAY** (e.g. `Watching WSH vs PHI`) when a live Capitals game is on (polled from NHL schedule); otherwise **Nothing :(**.

Goal announcements are **rich embeds**: 🚨 GOAL! 🚨 title, Ovechkin image thumbnail, and career goal count.

### Inject a test goal (see the Announcer react)

You can push a fake “goal increased” event into the stream and watch the Announcer log it. The **Ingestor** only writes when the real NHL API shows a higher count; it does not react to manually added messages.

```bash
./scripts/inject-test-goal.sh
```

Then check the Announcer container logs. To inject manually: `docker compose exec redis redis-cli XADD ovechkin:goals '*' payload '{"player_id":8471214,"goals":999,"recorded_at":"2025-02-22T12:00:00Z"}' goals 999`

## Run locally (without Docker)

1. Start Redis (e.g. `docker run -d -p 6379:6379 redis:7-alpine` or use Redis Stack).
2. From repo root:

```bash
go run ./ingestor/cmd/ingestor    # terminal 1
go run ./collector/cmd/collector  # terminal 2
go run ./predictor/cmd/predictor  # terminal 3
go run ./announcer/cmd/announcer  # terminal 4
```

Env: `REDIS_ADDR` (default `redis:6379` in compose), `POLL_INTERVAL` (ingestor), `COLLECTOR_INTERVAL` (collector, default 6h), `ODDS_API_KEY` (optional; predictor fetches Ovi anytime goal scorer odds). Discord vars: see table above.

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
