# RCJV Game Master

Tournament management for RoboCup Junior events, with a Go/Fiber/GORM API and Vue 3/TypeScript application styled with Tailwind CSS. The application supports shared main-league fields, sub-league rules and teams, stage generation, conflict-checked scheduling, signed referee reports, editor approval, and public results and presentation screens.

## Run locally

Use Go 1.27.1 or later, Node 22.18+ or 24.12+, and PostgreSQL. Keep your existing `config.toml`; it is ignored by Git. `config.example.toml` documents the settings without containing credentials. Set `RCJV_CONFIG=/path/to/config.toml` to use another configuration file.

From the repository root:

```sh
go run ./src
```

In another terminal:

```sh
cd frontend
npm ci
npm run dev
```

Open `http://localhost:2006`. Vite proxies `/api`, `/auth`, `/rest`, and `/healthcheck` to the backend on port 2006. The server migrates its application tables in the configured PostgreSQL database at startup.

For FusionAuth, register the exact configured `/auth/callback` URL and grant application users one or more roles: `admin`, `editor`, `referee`, `viewer`. Role matching is case insensitive. `auth.required_role`, if set, is an additional registration requirement. The frontend and callback must use the same hostname and scheme. The example configuration uses `localhost` on port 2005; use that hostname consistently when signing in.

For development without FusionAuth, set `disable_fusionauth = true` in a separate local configuration. This grants administrator access and binds the backend to `127.0.0.1`. Sessions and OAuth tokens remain on the backend; the browser receives an HTTP-only cookie. The session store is in memory, so restarting the process signs users out. Run one backend instance with this implementation.

## Production build

```sh
cd frontend
npm ci
npm run build
cd ..
go build -o rcjv-gamemaster ./src
./rcjv-gamemaster
```

Run from the repository root so the executable can find `config.toml` and `frontend/dist`. When that build exists, Fiber serves the frontend and API together on port 2006. Use an HTTPS reverse proxy and configure the public FusionAuth callback and frontend URLs for that origin. Set `database.sslmode` as required by your PostgreSQL installation; omitting it preserves the outline's `disable` default. Database passwords are URL encoded, including passwords containing special characters.

## Event workflow

1. Create a tournament with its abbreviation, dates, and IANA timezone. Select it as the active workspace.
2. Open **Tournament setup → Main leagues** and create a main league such as Soccer or Rescue. This form contains its management mode (full match management or scheduling only) and referee control. Use **Edit main league** beside an existing main league to change these settings.
3. After saving a main league, continue to **Sub-leagues** and add its competition categories, such as Infrared or Vision. Select the existing main league and use **Enable robot inspections** to choose whether this sub-league requires pregame inspections. Disabling inspections skips the referee checklist and retains saved limits; robot count remains available for timeout controls. Match-clock inputs are disabled when the main league's referee control is off, retaining durations for later use. Management mode and referee control are inherited from the main league. Create sub-leagues before adding their teams and stages. Existing leagues keep inspections enabled by default.
4. Add teams, referees, fields, and stages. A main-league field is available to its sub-leagues. Link each referee's FusionAuth user UUID to their roster entry. Availability bounds apply to the entire duration of an assigned match.
5. Create field timeslots from a time window and slot length. Copy a field's availability to another field in the same league. Input times use the device timezone; the schedule displays the tournament timezone.
6. Create matches manually or generate a round robin, Swiss round, or knockout round. Swiss pairings use approved standings, avoid rematches, and rotate byes; generation reports when no valid pairing remains. Knockouts seed by standings and advance winners. Set a stage's qualifying-team limit to 4 for semifinals or 2 for a final. A source stage must precede the destination stage, and its results must be approved.
7. Use the schedule's day tabs and field grid to drag matches into timeslots, or open a match and select its slot and referees. The server rejects incompatible fields, short slots, team overlaps, referee overlaps, and unavailable referees. Scheduling-only leagues support single-team runs. Publish schedules when ready.
8. A referee opens their assigned match from the **Referee desk**, records checks for each team robot if inspections are enabled, chooses initial sides and kickoff, and starts play. With inspections disabled, the referee goes directly to sides and kickoff setup. At least one referee must be assigned before kickoff. Match time, score, checks, and robot timeouts are saved on the server and survive refreshes. Tapping an expired first half starts halftime; tapping the expired break prepares the second half, which starts on command with sides and kickoff switched. Robot timeouts last one minute and reset when tapped again.
9. At full time, both team representatives and every assigned referee draw their signatures and submit the report. An editor can return it for correction or approve it. Approval publishes the result automatically. The report retains the original signed score if an editor amends the result; returned reports require new signatures. Unreviewed final scores, signatures, inspection records, account links, referee availability, and private notes are excluded from public responses. Live scores are visible while a published match is in progress.

Administrators manage setup and matches. Editors manage match details, scheduling, publication, and result review. Referees control games assigned to their linked account. Viewers can read the private schedule. All permissions and scheduling checks are enforced by the API.

## Public pages and existing soccer statistics

For a tournament with abbreviation `RCJ2027`:

- Public schedule, teams, and standings: `/#/public/RCJ2027`
- Large-screen scoreboard, countdown, and upcoming games: `/#/display/RCJ2027`
- Public JSON workspace: `/api/public/RCJ2027`
- Catigoal-compatible API base: `/rest/v1/RCJ2027/`

The compatibility API provides `leagues`, `matches`, and `standings`, with `league` and zero-based `league_stage` filters. Its default responses match the live Catigoal envelopes and the adjacent `RCSoccerStats` client's decoding types. Point that application's tournament API URL at this server's `/rest/v1/{abbreviation}/` base. No changes were made to the existing statistics project or Catigoal account.

The supplied OpenAPI document describes an older array-based response shape. Append `format=openapi` to compatibility requests to receive that shape. Both formats include only published matches and approved final results. Standings use three points for a win, one for a draw, and three for a generated bye, then goal difference, goals scored, and team ID for deterministic tie breaking. A manually created single-team run is not a bye.

## Verification

Tailwind CSS 4 is integrated through its official Vite plugin. Shared layouts and components use utilities and `@apply`; event-specific field-grid, referee-console, and display styling remains in component styles. Theme colors live in `frontend/src/assets/main.css` under `@theme`. The component layer allows utility classes to override shared styles.

```sh
go test ./src/...
go vet ./src/...
cd frontend
npm run build
npm run lint
```

The integration tests use temporary in-memory SQLite databases and require CGO and a C compiler. Production uses PostgreSQL. Tests cover tournament creation, pair generation, scheduling conflicts, role restrictions, CSRF, referee account assignments, inspections, clock transitions, timeout resets, signatures, submission locks, approval/publication, compatibility responses, and public-data redaction. They do not contact the configured PostgreSQL or FusionAuth services. On a host with a read-only default Go build cache, set `GOCACHE` to a writable directory.

The implementation contract in `IMPLEMENTATION.md` documents the private entity and referee endpoints. `GET /healthcheck` reports process health.
