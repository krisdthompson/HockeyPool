"""Extract ESPN's fantasy hockey cheat sheet (PDF) into players/espn.csv.

    pip install pypdf cffi
    python3 players/espn_parse.py fhl2026cheatsheet.pdf > players/espn.csv

Output: one row per skater per list, with ESPN's forward/defense tier and
listing order. Goalie lists are dropped (points-only pool).
"""
import csv
import re
import sys

from pypdf import PdfReader

text = "\n".join(p.extract_text(extraction_mode="layout") for p in PdfReader(sys.argv[1]).pages)
lines = text.replace("\u00a0", " ").splitlines()
HEAD = {
  "Allen's Forward Tiers": "F", "Allen's Defensemen Tiers": "D",
  "Wyshynski's \"Plant My Flag\" List": "Flag", "Matiash's Top Sleepers": "Sleeper",
  "Allen's Bounceback Picks": "Bounceback", "Matash's Rookies To Know": "Rookie",
  "Allen's Breakout Picks": "Breakout", "Matiash's Goalie Picks": "GOALIE",
  "Allen's Late Round Picks": "LateRound",
  "More valuable in H2H leagues": "H2H", "More valuable in roto leagues": "Roto",
  "More valuable in points leagues": "PointsLeague",
}
def bucket(x): return 0 if x < 45 else 1 if x < 95 else 2 if x < 145 else 3
cur = {}
out = []  # (section, tier, name, pos, team)
tier = {}
for ln in lines:
    for m in re.finditer(r'\S+(?: {1,2}\S+)*', ln):
        seg, x = re.sub(r'\s+', ' ', m.group(0)), m.start()
        b = bucket(x)
        if seg in HEAD:
            cur[b] = HEAD[seg]; continue
        sec = cur.get(b)
        if not sec: continue
        mt = re.match(r'^(\d+)\s+(.*)$', seg)
        if mt and sec in ('F', 'D'):
            tier[sec] = int(mt.group(1)); seg = mt.group(2)
        parts = [p.strip() for p in seg.split(',')]
        if len(parts) < 2: continue  # subtitles
        name = parts[0]
        if sec == 'D':
            pos, team = 'D', parts[1]
        elif len(parts) >= 3:
            pos, team = parts[1], parts[2]
        else:
            continue
        if sec in ('F','D') and len(parts) < (2 if sec=='D' else 3): continue
        out.append(dict(sec=sec, tier=tier.get(sec), name=name, pos=pos, team=team))
w = csv.writer(sys.stdout, lineterminator="\n")
w.writerow(["List", "Tier", "Order", "Name", "Pos", "Team"])
order = {}
for o in out:
    if o["pos"] == "G" or o["sec"] == "GOALIE" or not re.fullmatch(r"[A-Z]{2,4}", o["team"]):
        continue
    order[o["sec"]] = order.get(o["sec"], 0) + 1
    w.writerow([o["sec"], o["tier"] or "", order[o["sec"]], o["name"], o["pos"], o["team"]])
