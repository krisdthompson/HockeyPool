# hockeypool

Live draft tracker for a points-only (G + A) hockey pool. It's one Go binary
with no dependencies and an embedded web page. Everyone in the room can open
the same URL, and picks sync every 2 seconds.

## What it does

- **Player list**: the organizer's pick sheet (`players/*.xlsx`) is converted
  by `players/convert.py` into `players/players.csv`, which is built into the
  app and loaded automatically when the draft has no players. Injury "out
  until" dates become expected games missed. The season is assumed to open
  Oct 7 and play 82 games in 186 days. Rookies are hand-flagged in the script
  as a best guess. To regenerate:
  `python3 players/convert.py players/<sheet>.xlsx > players/players.csv`.
- **ESPN cheat sheet**: `players/espn_parse.py` extracts ESPN's tiers and
  lists (sleepers, breakouts and so on) from the PDF into `players/espn.csv`.
  `convert.py` then turns each ESPN forward or defense rank into an
  "Expert" points estimate: ESPN's Nth forward gets the Nth-best forward
  points total from last season. The app blends that into Proj ("ESPN tier
  weight", default 50%). ESPN's labels appear in each player's note.
- Before the first pick, each deploy with a changed built-in list reloads
  the players. Once the draft starts, only Setup changes them.
- **More lists**: in Setup, **Merge into list** matches players by name and
  updates only the columns your list has (for example `Name,Proj` or
  `Name,Rookie`). Players it doesn't know are added. Merging is safe
  mid-draft. Recognised headers:
  `Name, Team, Pos, GP, G, A, Pts, Proj, Miss, Rookie, Injury, Note`.
- **Ranks** available players by a score:
  `Proj × injury × rookie × (1 + stack% × my players on that team) × team-cap`.
  - Proj: the list's `Proj` if given. Otherwise it's a blend of last
    season's points and 82-game pace ("Trust 82-game pace", default 50%).
    Rookies under 40 GP blend toward a baseline (default 30 pts), so a hot
    9-game cameo doesn't rank as a star.
  - Injury: `Miss` scales by (82 − miss) / 82. Otherwise IR, Out or Season
    in the injury text takes the IR discount, and anything else takes the
    day-to-day discount.
  - Stacking: +10% per player you already own from that NHL team. The
    optional **Max NHL teams** discounts players who would add another team.
  - All weights are sliders, saved per browser.
- **Auction values**: in auction mode each available player shows **Value**
  (a fair price for you right now), **Good ≤** (85% of value: a bargain)
  and **Max** (value +15%, or +30% for your ★ targets, capped at what you
  can bid). Value splits the room's remaining money, beyond the minimum bid
  for every open spot, by points above the best player who won't be
  bought. It recalculates after every sale, and it includes your stack
  bonus.
- **Flags**: ★ target, ⚠ avoid and caution, with the reason in the note.
  They're set in `players/convert.py` (TAGS) and editable per player in
  the app.
- **Tick off drafted players**: each row has a ✓ checkbox. Ticking it
  removes the player from the board without recording a buyer; unticking
  (with "Show drafted" on) puts him back. **Mine** records your own buys
  with a price, which tracks your budget. A buyer and price for anyone
  else is optional (click the name). Unrecorded prices are assumed to be
  the player's pre-auction value when the app estimates the room's
  remaining money.
- **Best buys now**: the top five available players you can afford (no
  ⚠ avoid), plus your ★ targets, with Value and Max.
- **Add player**: search for a name that isn't listed, and a button adds it.
- **Draft or auction**: set an **Auction budget** in Setup (for example
  $100) to record a price with every pick. The page then shows each team's
  money left and max bid, keeping $1 for every open slot, and rejects
  overbids. With a budget of 0 it's a snake or straight draft: it shows
  who's on the clock, how many picks until your turn, and "likely gone"
  tags.
- Board grid, recent picks, undo, per-player edits (injury, rookie, note),
  and a JSON backup and restore.

## Logins

The browser asks for a username and password.

- **kris** (the admin) sees everything: live recommendations, values, max
  bids, ★/⚠ flags and their reasons, Setup, and ticking players off.
- **Any other username** with the guest password gets a read-only board:
  search, type-ahead and stats, with no recommendations, values or flags.
  The server doesn't even send guests the admin's flags and reasons.

Passwords live in `fly.toml` only as PBKDF2 hashes (`DRAFT_ADMIN_HASH`,
`DRAFT_GUEST_HASH`). Make a new one with `go run . hash <password>`. A
`DRAFT_PASSWORD` fly secret also works for the admin. With nothing
configured, the site is open and everyone is the admin.

## Run locally

    go run .            # http://localhost:8080, state in ./data/state.json

With no hashes or `DRAFT_PASSWORD` set, the local site is open.

## Deploy to fly.io

    fly auth login
    fly apps create hockeypool-draft          # or edit `app` in fly.toml
    fly volumes create draft_data --region dfw --size 1 --yes
    fly secrets set DRAFT_PASSWORD=pick-a-password
    fly deploy

The app runs as one always-on shared-cpu machine with a 1 GB volume. It costs
cents for a night. Run `fly apps destroy hockeypool-draft` when the pool is
done.

The site is also served at https://hockeypool.opeongo.net. That DNS name is
a CNAME to `hockeypool-draft.fly.dev`, and the deploy workflow requests its
TLS certificate (`fly certs add hockeypool.opeongo.net`).

Or push to GitHub: `.github/workflows/fly-deploy.yml` deploys on every push to
`main` once the repo has a `FLY_API_TOKEN` secret (`fly tokens create org`).
It creates the app and volume on the first run. Run the `fly secrets set`
line yourself once.
