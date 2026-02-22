#!/usr/bin/env bash
# Inject a test goal event into the Redis stream so the Announcer picks it up.
# Run with: ./scripts/inject-test-goal.sh
# Requires: docker compose (or docker-compose) and the stack running (redis + announcer).

set -e
REDIS_SERVICE="${REDIS_SERVICE:-redis}"
# Same format as Ingestor: payload (JSON) + goals
PAYLOAD='{"player_id":8471214,"goals":999,"recorded_at":"2025-02-22T12:00:00Z"}'

if command -v docker &>/dev/null; then
  if docker compose exec -T "$REDIS_SERVICE" redis-cli XADD ovechkin:goals '*' payload "$PAYLOAD" goals 999; then
    echo "Test goal event added to stream ovechkin:goals. Watch Announcer logs for the notification."
  else
    # Fallback for docker-compose
    docker-compose exec -T "$REDIS_SERVICE" redis-cli XADD ovechkin:goals '*' payload "$PAYLOAD" goals 999
    echo "Test goal event added. Watch Announcer logs."
  fi
else
  echo "Inject via redis-cli: XADD ovechkin:goals * payload '$PAYLOAD' goals 999"
  echo "Then connect to your Redis (e.g. redis-cli -h localhost -p 6379) and run the above."
fi
