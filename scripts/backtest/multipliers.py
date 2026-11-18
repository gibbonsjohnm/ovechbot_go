"""Walk-forward backtest of the ovechbot predictor's per-game multipliers (predictHeuristic in model.go).

Each game from 2024-25 onward is predicted from earlier games only (2023-24 is prior-only) and from standings as of the
day before the game. Mirrors model.go / baseline.go (goalie factor omitted: no historical starter data).

Usage: python3 -I multipliers.py <gamelog dir with gl_<season>.json> <standings dir with YYYY-MM-DD.json>
"""
import datetime
import json
import math
import os
import sys

GL_DIR, ST_DIR = sys.argv[1], sys.argv[2]
SEASONS = ["20232024", "20242025", "20252026", "20262027"]

# ---- data -------------------------------------------------------------------------------------------------------------
log = []
for s in SEASONS:
    for g in reversed(json.load(open(os.path.join(GL_DIR, f"gl_{s}.json")))["gameLog"]):  # newest-first -> oldest-first
        log.append({"id": int(g["gameId"]), "date": g["gameDate"], "opp": g["opponentAbbrev"],
                    "home": g["homeRoadFlag"] == "H", "goals": int(g["goals"]), "shots": int(g["shots"])})

FIELDS = ["gamesPlayed", "goalAgainst", "goalFor", "pointPctg", "homeGamesPlayed", "homeGoalsAgainst",
          "roadGamesPlayed", "roadGoalsAgainst", "l10GamesPlayed", "l10GoalsAgainst", "l10GoalsFor"]
_st_cache = {}


def standings_before(date):
    d = (datetime.date.fromisoformat(date) - datetime.timedelta(days=1)).isoformat()
    if d not in _st_cache:
        raw = json.load(open(os.path.join(ST_DIR, f"{d}.json")))["standings"]
        _st_cache[d] = {t["teamAbbrev"]["default"]: {k: t.get(k, 0) or 0 for k in FIELDS} for t in raw}
    return _st_cache[d]


# ---- model (mirrors Go) -------------------------------------------------------------------------------------------------
def season_of(e):
    return e["id"] // 1_000_000


def baseline_gpg(gl, max_games=82, k_shots=3, k_pct=40, k_gpg=15):
    if not gl:
        return 0.4
    split = len(gl)
    cur = season_of(gl[-1])
    while split > 0 and season_of(gl[split - 1]) == cur:
        split -= 1
    current, prior = gl[split:][-max_games:], gl[:split][-max_games:]
    if not prior:
        return sum(e["goals"] for e in current) / len(current)
    cg, cs, n = sum(e["goals"] for e in current), sum(e["shots"] for e in current), len(current)
    pg, ps = sum(e["goals"] for e in prior), sum(e["shots"] for e in prior)
    if ps == 0:
        return (cg + k_gpg * pg / len(prior)) / (n + k_gpg)
    spg = (cs + k_shots * ps / len(prior)) / (n + k_shots)
    shp = (cg + k_pct * pg / ps) / (cs + k_pct)
    return spg * shp


def league_avg_ga(st):
    ga, gp = sum(t["goalAgainst"] for t in st.values()), sum(t["gamesPlayed"] for t in st.values())
    return ga / gp if st and gp else 3.0


def league_pace(st):
    gf = sum(t["goalFor"] for t in st.values())
    ga, gp = sum(t["goalAgainst"] for t in st.values()), sum(t["gamesPlayed"] for t in st.values())
    return (gf + ga) / (2 * gp) if st and gp else 3.0


def eff_opp_ga(t, caps_home):
    if t["gamesPlayed"] == 0:
        return 3.0
    vga, vgp = (t["roadGoalsAgainst"], t["roadGamesPlayed"]) if caps_home else (t["homeGoalsAgainst"], t["homeGamesPlayed"])
    full = t["goalAgainst"] / t["gamesPlayed"]
    l10 = t["l10GoalsAgainst"] / t["l10GamesPlayed"] if t["l10GamesPlayed"] >= 5 else None
    if vgp >= 5:
        v = vga / vgp
        return 0.7 * v + 0.3 * l10 if l10 is not None else v
    return 0.7 * full + 0.3 * l10 if l10 is not None else full


def clamp(x, lo, hi):
    return max(lo, min(hi, x))


def factors(gl, g, st, bgpg):
    """Return the deployed multipliers for game g given prior log gl and prior-day standings st."""
    f = {}
    t = st.get(g["opp"])
    f["opp"] = clamp(eff_opp_ga(t, g["home"]) / league_avg_ga(st), 0.75, 1.35) if t and t["gamesPlayed"] > 0 else 1.0
    f["home"] = 1.05 if g["home"] else 0.95
    # Ovi vs opponent: last 10 meetings
    goals = games = 0
    for e in reversed(gl):
        if games >= 10:
            break
        if e["opp"] == g["opp"]:
            games += 1
            goals += e["goals"]
    f["vsOpp"] = clamp(goals / games / bgpg, 0.85, 1.15) if games >= 3 and bgpg > 0 else 1.0
    f["ptStr"] = clamp(1.04 - 0.08 * t["pointPctg"], 0.92, 1.08) if t and t["pointPctg"] > 0 else 1.0
    if t and t["l10GamesPlayed"] >= 5 and league_pace(st) > 0:
        pace = (t["l10GoalsFor"] + t["l10GoalsAgainst"]) / (2 * t["l10GamesPlayed"])
        f["pace"] = clamp(pace / league_pace(st), 0.97, 1.03)
    else:
        f["pace"] = 1.0
    days = (datetime.date.fromisoformat(g["date"]) - datetime.date.fromisoformat(gl[-1]["date"])).days
    f["rest"] = 0.92 if days <= 1 else (1.02 if days >= 3 else 1.0)
    return f


NAMES = ["opp", "home", "vsOpp", "ptStr", "pace", "rest"]

# ---- precompute per-game inputs -----------------------------------------------------------------------------------------
rows, missing_opp, early = [], set(), 0
start = next(i for i, e in enumerate(log) if season_of(e) == 2024)
for i in range(start, len(log)):
    gl, g = log[:i], log[i]
    st = standings_before(g["date"])
    if g["opp"] not in st:
        missing_opp.add((g["date"], g["opp"]))
    elif st[g["opp"]]["gamesPlayed"] < 5:
        early += 1
    b = baseline_gpg(gl)
    rows.append({"season": season_of(g), "y": 1 if g["goals"] > 0 else 0, "base": 1 - math.exp(-b),
                 "f": factors(gl, g, st, b), "date": g["date"], "opp": g["opp"]})

SEAS = sorted({r["season"] for r in rows})


def prob(r, strengths, rounded=True):
    """strengths: name -> s; multiplier becomes 1 + s*(f-1). Missing name -> s=0 (multiplier off)."""
    p = r["base"]
    for k, s in strengths.items():
        p *= 1 + s * (r["f"][k] - 1)
    if rounded:  # deployed: ClampPct(round(prob*100))
        return clamp(round(p * 100), 5, 75) / 100
    return clamp(p, 0.05, 0.75)


def losses(strengths, rounded=True):
    out = []
    for r in rows:
        p = prob(r, strengths, rounded)
        out.append((-(r["y"] * math.log(p) + (1 - r["y"]) * math.log(1 - p)), (p - r["y"]) ** 2, p))
    return out


def summarize(strengths, ref=None, rounded=True):
    L = losses(strengths, rounded)
    res = {}
    for s in SEAS + ["all"]:
        idx = [i for i, r in enumerate(rows) if s == "all" or r["season"] == s]
        ll = sum(L[i][0] for i in idx) / len(idx)
        br = sum(L[i][1] for i in idx) / len(idx)
        res[s] = (ll, br)
    se = None
    if ref is not None:  # paired SE of overall log-loss difference vs ref
        d = [a[0] - b[0] for a, b in zip(L, ref)]
        m = sum(d) / len(d)
        se = math.sqrt(sum((x - m) ** 2 for x in d) / (len(d) - 1) / len(d))
    return res, L, se


def fmt_row(name, res, base_res=None, se=None):
    cells = []
    for s in SEAS + ["all"]:
        ll, br = res[s]
        if base_res is None:
            cells.append(f"{ll:.4f}")
        else:
            cells.append(f"{ll:.4f}({ll - base_res[s][0]:+.4f})")
    br = res["all"][1]
    extra = f"  brier {br:.4f}"
    if se is not None:
        extra += f"  dSE {se:.4f}"
    return f"{name:28} " + " ".join(f"{c:>17}" for c in cells) + extra


def main():
    print(f"games evaluated: {len(rows)}  per season: " +
          ", ".join(f"{s}-{(s + 1) % 100:02d}: n={sum(1 for r in rows if r['season'] == s)} "
                    f"hit={sum(r['y'] for r in rows if r['season'] == s)}" for s in SEAS))
    print(f"opponent missing from standings: {sorted(missing_opp) or 'none'}; games vs opp with <5 GP: {early}")
    for k in NAMES:
        vals = [r["f"][k] for r in rows]
        print(f"  {k:6} mean {sum(vals) / len(vals):.3f}  min {min(vals):.3f}  max {max(vals):.3f}  "
              f"non-1 {sum(1 for v in vals if abs(v - 1) > 1e-9)}")
    all_on = {k: 1.0 for k in NAMES}
    for rounded in (True, False):
        print(f"\n===== {'deployed (rounded int %, clamp 5-75)' if rounded else 'unrounded (clamp 0.05-0.75)'} =====")
        print(f"{'model':28} " + " ".join(f"{(str(s) + '-' + str((s + 1) % 100)):>17}" for s in SEAS) + f" {'all':>17}")
        base_res, base_L, _ = summarize({}, rounded=rounded)
        allr, all_L, se = summarize(all_on, base_L, rounded)
        print(fmt_row("baseline only", base_res))
        print(fmt_row("all (deployed, no goalie)", allr, base_res, se))
        print("-- baseline x one multiplier (delta vs baseline)")
        for k in NAMES:
            r, _, se = summarize({k: 1.0}, base_L, rounded)
            print(fmt_row(f"base x {k}", r, base_res, se))
        print("-- all minus one (delta vs all; negative = removing helps)")
        for k in NAMES:
            r, _, se = summarize({n: 1.0 for n in NAMES if n != k}, all_L, rounded)
            print(fmt_row(f"all - {k}", r, allr, se))
        print("-- strength scaling 1+s*(f-1), others at deployed (delta vs all)")
        for k in NAMES:
            for s in (0.0, 0.5, 1.5, 2.0):
                st = dict(all_on)
                st[k] = s
                r, _, se = summarize(st, all_L, rounded)
                print(fmt_row(f"all, {k} s={s}", r, allr, se))
        print("-- strength scaling on baseline alone (delta vs baseline)")
        for k in ("opp", "home"):
            for s in (0.5, 1.0, 1.5, 2.0):
                r, _, se = summarize({k: s}, base_L, rounded)
                print(fmt_row(f"base x {k} s={s}", r, base_res, se))
        if not rounded:
            break


if __name__ == "__main__":
    main()
