"""Convert the organizer's pick sheet (.xlsx) into players.csv for the app.

    pip install openpyxl
    python3 players/convert.py "players/<pick sheet>.xlsx" > players/players.csv

Stats are last season's. Injury "out until" dates become expected games
missed, assuming the season opens SEASON_START and runs 82 games over
SEASON_DAYS days. ROOKIES and PROJ_OVERRIDES are hand-maintained guesses;
fix them here or in the app.
"""
import csv
import datetime as dt
import math
import re
import sys

import openpyxl

SEASON_START = dt.date(2026, 10, 7)
SEASON_DAYS = 186

# Under 25 NHL games last season and (to my knowledge) still rookie-eligible.
ROOKIES = {
    "Ryan Ufko", "Porter Martone", "Cole Hutson", "Anton Frondell", "Alex Bump",
    "Konsta Helenius", "Ilya Protas", "Samuel Honzek", "Bradly Nadeau",
    "Carter Yakemchuk", "James Hagens", "Victor Eklund", "Michael Brandsegg-Nygard",
    "Tristan Luneau", "Luca Cagnoni", "Gavin McKenna", "Ivar Stenberg",
    "Roman Kantserov", "Viggo Bjorck", "Tij Iginla", "Vitali Pinchuk",
    "Trevor Connelly", "Charlie Stramel",
}

# Players whose last-season line says nothing useful about this season.
PROJ_OVERRIDES = {
    "Aleksander Barkov": (70, "Missed last season (knee); ~80-pt pace before that"),
}


def games_missed(until: dt.date) -> int:
    days = (until - SEASON_START).days
    return max(0, math.ceil(days * 82 / SEASON_DAYS))


def parse_until(status: str):
    m = re.search(r"until at least (\w{3}) (\d{1,2})", status)
    if not m:
        return None
    month = dt.datetime.strptime(m.group(1), "%b").month
    year = 2026 if month >= 7 else 2027
    return dt.date(year, month, int(m.group(2)))


def injury_fields(kind: str, status: str):
    """Returns (injury label, games missed, note)."""
    kind, status = (kind or "").strip(), (status or "").strip()
    if not kind and not status:
        return "", 0, ""
    if kind == "Suspension":
        m = re.match(r"(\d+)", status)
        n = int(m.group(1)) if m else 0
        return f"Susp {n}g", n, status
    if status.startswith("Day-to-Day"):
        return f"DTD {kind}", 0, ""
    until = parse_until(status)
    if until is None:
        return kind, 0, status
    miss = games_missed(until)
    when = until.strftime("%b %-d")
    if miss == 0:
        # Back before opening night: keep it visible, day-to-day level risk.
        return f"Camp: {kind}, back ~{when}", 0, ""
    return f"Out: {kind}, to ~{when}", miss, ""


def main(path: str):
    ws = openpyxl.load_workbook(path, data_only=True).active
    header = [str(c.value or "").strip() for c in ws[1]]
    col = {h: i for i, h in enumerate(header) if h}
    out = csv.writer(sys.stdout, lineterminator="\n")
    out.writerow(["Name", "Team", "Pos", "GP", "G", "A", "Pts", "Proj", "Miss", "Rookie", "Injury", "Note"])
    for row in ws.iter_rows(min_row=2, values_only=True):
        name = row[col["PLAYER NAME"]]
        if not name or not row[col["POS"]]:
            continue  # skips the Spent/Left rows
        name = str(name).strip()
        team = row[col["TEAM"]]
        injury, miss, note = injury_fields(row[col["Injury"]], row[col["Status"]])
        proj = ""
        if name in PROJ_OVERRIDES:
            proj, note = PROJ_OVERRIDES[name]
        if team == "UFA":
            note = "Unsigned free agent"
        stat = lambda k: "" if row[col[k]] is None else row[col[k]]
        out.writerow([name, team, row[col["POS"]], stat("GP"), stat("G"), stat("A"), stat("Pts"),
                      proj, miss or "", "Y" if name in ROOKIES else "", injury, note])


if __name__ == "__main__":
    main(sys.argv[1])
