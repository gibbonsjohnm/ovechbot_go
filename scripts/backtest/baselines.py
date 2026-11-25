"""Backtest Ovechkin per-game scoring-probability baselines on his NHL game logs.

Each game is predicted only from games before it. Usage: python3 -I backtest.py <dir with gl_<season>.json>
"""
import json
import math
import sys

d = sys.argv[1]
games = []
for s in ["20232024", "20242025", "20252026", "20262027"]:
    for g in reversed(json.load(open(f"{d}/gl_{s}.json"))["gameLog"]):
        games.append({"season": int(s[:4]), "g": g["goals"], "s": g["shots"]})


def p_from_xg(xg):
    return 1 - math.exp(-max(xg, 0))


def split(prior, nxt):
    if nxt["season"] != prior[-1]["season"]:  # first game of a new season: last season becomes the prior
        return [], prior[-82:]
    cur = [x for x in prior if x["season"] == nxt["season"]]
    prev = [x for x in prior if x["season"] < nxt["season"]][-82:]
    return cur, prev


def old_82(prior, nxt):
    last = prior[-82:]
    return p_from_xg(sum(x["g"] for x in last) / len(last))


def gpg_shrunk(k):
    def f(prior, nxt):
        cur, prev = split(prior, nxt)
        pg = sum(x["g"] for x in prev) / len(prev)
        return p_from_xg((sum(x["g"] for x in cur) + k * pg) / (len(cur) + k))
    return f


def shot_based(k_shots, k_pct):
    """xG/game = shots/game (current season shrunk toward prior by k_shots games) x sh% (shrunk toward prior by k_pct shots)."""
    def f(prior, nxt):
        cur, prev = split(prior, nxt)
        spg_prev = sum(x["s"] for x in prev) / len(prev)
        shp_prev = sum(x["g"] for x in prev) / max(1, sum(x["s"] for x in prev))
        cs, cg, n = sum(x["s"] for x in cur), sum(x["g"] for x in cur), len(cur)
        spg = (cs + k_shots * spg_prev) / (n + k_shots)
        shp = (cg + k_pct * shp_prev) / (cs + k_pct)
        return p_from_xg(spg * shp)
    return f


models = {
    "old: last-82 GPG": old_82,
    "PR#2: GPG k=30": gpg_shrunk(30),
    "GPG k=15": gpg_shrunk(15),
    "shots k=10, sh% k=150": shot_based(10, 150),
    "shots k=5, sh% k=200": shot_based(5, 200),
    "shots k=5, sh% k=100": shot_based(5, 100),
    "shots k=3, sh% k=60": shot_based(3, 60),
}

start = next(i for i, x in enumerate(games) if x["season"] == 2024)
print(f"games evaluated: {len(games) - start} (2024-25 onward); this season: {sum(1 for x in games if x['season'] == 2026)}")
print(f"{'model':24} {'logloss all':>11} {'brier all':>9} {'logloss 26-27':>13} {'mean p 26-27':>12} {'next game':>9}")
for name, f in models.items():
    ll = br = ll26 = p26 = 0.0
    n = n26 = 0
    for i in range(start, len(games)):
        p = min(max(f(games[:i], games[i]), 1e-4), 1 - 1e-4)
        y = 1 if games[i]["g"] > 0 else 0
        loss = -(y * math.log(p) + (1 - y) * math.log(1 - p))
        ll += loss
        br += (p - y) ** 2
        n += 1
        if games[i]["season"] == 2026:
            ll26 += loss
            p26 += p
            n26 += 1
    nxt = f(games, {"season": games[-1]["season"]})
    print(f"{name:24} {ll / n:11.4f} {br / n:9.4f} {ll26 / n26:13.4f} {100 * p26 / n26:11.1f}% {100 * nxt:8.1f}%")
