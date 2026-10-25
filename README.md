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
6. Saving an automatic stage creates empty games immediately in both full-management and scheduling-only leagues, so you can schedule them before assigning teams. **Expected teams** sets the planning count before registration is complete; 0 uses the registered sub-league teams. **Qualifying teams** caps that count. Round robin reserves all pairings; Swiss and knockout reserve one round per stage, so semifinals and the final are separate stages. **Individual runs** reserves one game per team with no opponent; even a single-team stage is supported. Runs are never treated as byes or awarded bye points. Registration changes resize unused stages automatically; reserved games are retained. Only **Manual games** stages use manual game creation. New stage forms default to **Individual runs** for scheduling-only sub-leagues; you can select a pairing system instead. In **Tournament setup → Stages**, use **Populate teams** to open the stage's assignment workspace. Review the proposed pairings and confirm them to fill the existing games while keeping their times, fields, referees, and publication. Assignment rejects team or referee overlaps without changing any games. Swiss uses approved standings and avoids rematches; knockout advances winners or seeds from standings. Set the qualifying-team limit to 4 for semifinals or 2 for a final. Source results must be approved. **Clear draw** resets automatic games to awaiting teams while preserving their bookings; for manual games it removes the saved rows. Started or reviewed games block clearing, except automatic byes. Clear populated dependent stages first.
7. Use the schedule's day tabs and field grid to drag matches into timeslots, or open a match and select its slot and referees. The server rejects incompatible fields, short slots, team overlaps, referee overlaps, and unavailable referees. Choose **Individual runs** in stage settings for automatic single-team runs; manual scheduling-only stages also allow a run without an opponent. Publish schedules when ready. The **Main league** filter shows that league’s fields and all its sub-leagues. Your browser remembers this choice separately for each tournament, including after a refresh.
8. A referee opens their assigned match from the **Referee desk**, records checks for each team robot if inspections are enabled, chooses initial sides and kickoff, and starts play. With inspections disabled, the referee goes directly to sides and kickoff setup. At least one referee must be assigned before kickoff. Match time, score, checks, robot timeouts, and match events are saved on the server and survive refreshes. Tapping an expired first half starts halftime; tapping the expired break prepares the second half, which starts on command with sides and kickoff switched. Robot timeouts last one minute: tap the robot or **Reset to 1:00** to restart, and use **Return to play · clear** to remove the timer. **Skip to…** advances a period after confirmation. **End match early** requires a reason, preserves the score, and proceeds to signatures and editor review.
9. At full time, both team representatives and every assigned referee draw their signatures and submit the report. An editor can return it for correction or approve it. Approval publishes the result automatically. The report retains the original signed score if an editor amends the result; returned reports require new signatures. Unreviewed final scores, signatures, inspection records, account links, referee availability, and private notes are excluded from public responses. Live scores are visible while a published match is in progress.

Administrators manage setup and matches. Editors manage match details, scheduling, publication, and result review. Referees control games assigned to their linked account. Viewers can read the private schedule. All permissions and scheduling checks are enforced by the API.

## Public pages and existing soccer statistics

For a tournament with abbreviation `RCJ2027`:

- Public schedule, teams, and standings: `/#/public/RCJ2027`
- Large-screen scoreboard, countdown, and upcoming games: `/#/display/RCJ2027`
- Display for a single field: `/#/display/RCJ2027/fields/3` or `/#/display/RCJ2027?field=3`
- Public JSON workspace: `/api/public/RCJ2027`
- Catigoal-compatible API base: `/rest/v1/RCJ2027/`

The public tournament page first asks visitors to choose a main league or sub-league. **Change league** returns to the chooser; the selected league is stored in the URL. A main-league board includes its sub-leagues. The standings tab shows only the latest published table for each selected league and its publication time.

Open the private **Standings** tab to control publication. **Auto publish latest** is enabled by default for each league and uses the newest stage with at least one approved, public real result. Empty future stages and automatic byes alone do not replace the table. Turn auto mode off to freeze current tables, then use **Publish standings** or **Republish standings** to save a stage's current table. **Unpublish standings** withdraws it; the public board falls back to the latest remaining publication. A retracted or unpublished source result withdraws the affected manual snapshot. Match publication and standings publication are separate.

In **Tournament setup → Fields & availability**, each field has **Open field display** and **Copy display link** actions. Screen displays have no field selector or fullscreen button; their URL chooses the field. Unknown field IDs show an unavailable state. The event log below upcoming games and recent results shows recorded goals, timeout starts/resets/returns, pauses, period advances, and match endings, newest first. Team colors reflect the side at the time of each event, including after halftime changes. Old matches without recorded events have an empty log. Private early-ending reasons remain with officials. Live goal events are visible during play; after a game finishes, its goal events and final score are hidden until editor approval.

The compatibility API provides `leagues`, `matches`, and `standings`. Its default object responses retain the Catigoal shapes used by `RCSoccerStats`. Configure that application's upstream base URL as this server's `/rest/v1/{abbreviation}/`, including the trailing slash. The adjacent client's current base URL is set in `backend/config/getConfig.go`; its tournament abbreviation alone does not change the upstream host.

The updated [OpenAPI specification](src/web/docs/rcgamemaster_restapi_v1_openapi.yaml) is also available from the running server at **`/rest/v1/openapi.yaml`**. It documents the actual object envelopes, main/sub-league catalog and filters, individual runs, published placeholders, and the optional legacy array format. The original `catigoal_restapi_v1_openapi.yaml` remains the supplied Catigoal reference.

For a two-level selector, request `/rest/v1/RCJ2027/leagues` and read `main_leagues`, then the selected main league's `sub_leagues`. These catalogs include leagues without stages. The existing flat `leagues` list continues to contain only leagues with stages, so older clients can keep using it. Use abbreviations from the catalog in requests:

```text
GET /rest/v1/RCJ2027/leagues?main_league=soccer
GET /rest/v1/RCJ2027/matches?main_league=soccer
GET /rest/v1/RCJ2027/matches?main_league=soccer&sub_league=ir&league_stage=0
GET /rest/v1/RCJ2027/standings?main_league=soccer&sub_league=ir&league_stage=0
```

`main_league` includes the main league's own games and its sub-leagues. `sub_league` selects one child and can also be used without `main_league`. The legacy `league=ir` parameter still selects exactly that league; `league=soccer` selects only games attached directly to that main league. If both `league` and `sub_league` are supplied, their abbreviations must match. A child selected with `main_league` must belong to that parent. Unknown leagues and conflicting selections return 404 with `{error:string}`.

`league_stage` is the zero-based position in the selected league's complete `league_stages` list, ordered by stage sort order and ID. Matches with a stage filter and all standings requests require one exact `league` or `sub_league`; a main league alone cannot identify a stage or one standings table. Omit `league_stage` for the selected league's latest published snapshot, under the legacy Seeding label; `published_stage_id`, `published_stage_name`, and `published_stage_number` identify its source stage. Standings from different sub-leagues are not combined. Stage filtering does not narrow the league catalog or renumber its stages.

Use the default object format for RCSoccerStats. `format=openapi` requests the original array wrappers; the updated specification describes both formats. REST responses include only published games, with goals and points exposed after approval. Published empty games carry `is_placeholder:true`; individual runs have `stage_type:individual`, `is_bye:false`, and a null opponent after assignment. A run earns no match or bye points. Standings award three points for a win, one for a draw, and three for a generated bye, then use goal difference, goals scored, and team ID to break ties. REST timestamps use the tournament's local timezone without a numeric offset; the envelope supplies `tournament_publishing_timezone`.

## Verification

Tailwind CSS 4 is integrated through its official Vite plugin. Shared layouts and components use utilities and `@apply`; event-specific field-grid, referee-console, and display styling remains in component styles. Theme colors live in `frontend/src/assets/main.css` under `@theme`. The component layer allows utility classes to override shared styles.

```sh
go test ./src/...
go vet ./src/...
cd frontend
npm run build
npm run lint
```

The integration tests use temporary in-memory SQLite databases and require CGO and a C compiler. Production uses PostgreSQL. Tests cover tournament creation, read-only pairing previews, stale submissions, batch rollback, automatic empty games in both league modes, individual runs, schedule-preserving team assignment, stage clearing and redraws, played-match and seeded-stage protections, scheduling conflicts, role restrictions, CSRF, referee account assignments, inspections, clock transitions, timeout reset/clear, early finishes, persisted events, signatures, submission locks, approval/publication, compatibility responses, and public-data redaction. They do not contact the configured PostgreSQL or FusionAuth services. On a host with a read-only default Go build cache, set `GOCACHE` to a writable directory.

The implementation contract in `IMPLEMENTATION.md` documents the private entity and referee endpoints. `GET /healthcheck` reports process health.
