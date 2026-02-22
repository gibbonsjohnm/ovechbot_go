#!/usr/bin/env bash
# Inject a test goal event into the Redis stream so the Announcer picks it up.
# Run with: ./scripts/inject-test-goal.sh
# Requires: Docker, the stack running (redis + announcer).

set -e
REDIS_SERVICE="${REDIS_SERVICE:-redis}"
PAYLOAD='{"player_id":8471214,"goals":999,"recorded_at":"2025-02-22T12:00:00Z"}'

run() {
  if docker compose exec -T "$REDIS_SERVICE" redis-cli XADD ovechkin:goals '*' payload "$PAYLOAD" goals 999 2>/dev/null; then
    return 0
  fi
  docker-compose exec -T "$REDIS_SERVICE" redis-cli XADD ovechkin:goals '*' payload "$PAYLOAD" goals 999
}

if run; then
  echo "Test goal event added to stream ovechkin:goals. Watch Announcer logs for the notification."
else
  echo "Could not run redis-cli. Ensure containers are up (docker compose up -d) and Redis service is named $REDIS_SERVICE."
  echo "Or run manually: redis-cli XADD ovechkin:goals * payload '$PAYLOAD' goals 999"
  exit 1
fi
