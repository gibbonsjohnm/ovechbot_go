# Prediction backtests

Walk-forward backtests behind the predictor's model (`predictor/internal/model`). Every game is predicted only from
earlier games (and, for `multipliers.py`, standings as of the day before), then scored with log-loss.

- `baselines.py` compares baseline candidates (last-82 goals/game, season-shrunk goals/game, shots/game × shooting %).
  The shot-based baseline won in every season and is what the predictor uses.
- `multipliers.py` tests the per-game adjustments the predictor used to apply (opponent goals-against, home ice,
  head-to-head, opponent point %, pace, rest). None helped in both full seasons, and together they raised log-loss
  from 0.6291 to 0.6575 over 2024-25 to 2026-27, so the predictor no longer applies them.
- `fetch_standings.py` downloads and caches the prior-day standings `multipliers.py` needs.

The Go test `TestBacktest_Baseline` re-checks the baseline on every CI run against `predictor/internal/model/testdata`.

## Rerun

```sh
mkdir -p data/nhl data/standings
for s in 20232024 20242025 20252026 20262027; do
  curl -sL "https://api-web.nhle.com/v1/player/8471214/game-log/$s/2" -o "data/nhl/gl_$s.json"
done
python3 -I scripts/backtest/baselines.py data/nhl
python3 -I scripts/backtest/fetch_standings.py data/nhl data/standings
python3 -I scripts/backtest/multipliers.py data/nhl data/standings
```
