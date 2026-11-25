"""Fetch NHL standings as of the day before each Ovechkin game (2024-25 onward) and cache them.

Usage: python3 -I fetch_standings.py <gamelog dir> <standings cache dir>
"""
import datetime
import json
import os
import subprocess
import sys
import time

gl_dir, out_dir = sys.argv[1], sys.argv[2]
os.makedirs(out_dir, exist_ok=True)
dates = set()
for s in ["20242025", "20252026", "20262027"]:
    for g in json.load(open(os.path.join(gl_dir, f"gl_{s}.json")))["gameLog"]:
        d = datetime.date.fromisoformat(g["gameDate"]) - datetime.timedelta(days=1)
        dates.add(d.isoformat())
for d in sorted(dates):
    path = os.path.join(out_dir, f"{d}.json")
    if os.path.exists(path) and os.path.getsize(path) > 100:
        continue
    out = subprocess.run(["curl", "-sL", "--max-time", "30", f"https://api-web.nhle.com/v1/standings/{d}"],
                         capture_output=True, check=True).stdout
    json.loads(out)  # validate before caching
    with open(path, "wb") as f:
        f.write(out)
    print(d, len(out), flush=True)
    time.sleep(0.4)
print("dates:", len(dates))
