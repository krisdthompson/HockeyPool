# hockeypool

Live draft tracker for a points-only (G + A) hockey pool. It's one Go binary
with no dependencies and an embedded web page. Everyone in the room can open
the same URL, and picks sync every 2 seconds.

## What it does

- **Import** your player list: paste CSV, or copy cells straight from Google
  Sheets or Excel. Only `Name` is required. Recognised headers:
  `Name, Team, Pos, GP, G, A, Pts, Proj, Miss, Rookie, Injury, Note`.
  If `Pts` is blank it uses G + A. If `Proj` is blank it uses the 82-game
  points pace.
- **Ranks** available players by a score:
  `Proj × injury × rookie × (1 + stack% × my players on that team) × team-cap`.
  - Injury: `Miss` (expected games missed) scales by (82 − miss) / 82.
    Otherwise an `Injury` text of IR, LTIR, Out, Season or Surgery takes the
    IR discount (default 50%), and anything else, such as DTD, takes the
    day-to-day discount (default 5%).
  - Rookie: a 10% risk discount by default. Set it negative to boost rookies.
  - Stacking: +10% per player you already own from the same NHL team, so the
    list steers you toward fewer teams. You can also set **Max NHL teams**
    in Setup: once your roster reaches that many teams, players from any
    other team are discounted.
  - You can change all of the weights live with the sliders. They are saved
    per browser.
- **Tracks the draft**: snake or straight order, who's on the clock, and how
  many picks until your turn. A "likely gone" tag marks players in the top N
  by projection, where N is the number of picks before your turn. There's a
  board grid, recent picks and undo. Click a player name to edit their
  injury, rookie flag or note, or to record a pick for any manager out of
  order.
- **Backup**: Setup → Download backup (JSON), and restore it later.

## Run locally

    go run .            # http://localhost:8080, state in ./data/state.json

Set `DRAFT_PASSWORD=...` to require HTTP basic auth (any username).

## Deploy to fly.io

    fly auth login
    fly apps create hockeypool-draft          # or edit `app` in fly.toml
    fly volumes create draft_data --region dfw --size 1 --yes
    fly secrets set DRAFT_PASSWORD=pick-a-password
    fly deploy

The app runs as one always-on shared-cpu machine with a 1 GB volume. It costs
cents for a night. Run `fly apps destroy hockeypool-draft` when the pool is
done.

Or push to GitHub: `.github/workflows/fly-deploy.yml` deploys on every push to
`main` once the repo has a `FLY_API_TOKEN` secret (`fly tokens create org`).
It creates the app and volume on the first run. Run the `fly secrets set`
line yourself once.
