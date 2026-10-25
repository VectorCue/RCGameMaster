# Implementation contract

The application uses the existing Go Fiber/GORM backend and Vue/TypeScript frontend. All private routes are under `/api/v1`, use the existing cookie session and `X-TAS-CSRF: 1` for mutations. JSON property names are snake_case. Entity IDs are numeric.

## Workspace API

- `GET /tournaments`, `POST /tournaments`, `PUT /tournaments/:id`, `DELETE /tournaments/:id`
- `GET /tournaments/:id/workspace` returns `{tournament, leagues, stages, fields, slots, teams, referees, matches, standings}`. Lists are always arrays. Standings entries include `league_id, stage_id, team_id, played, wins, draws, losses, scored, conceded, difference, points, rank`.
- `POST /tournaments/:id/{leagues,stages,fields,slots,teams,referees,matches}` creates an entity; `PUT /{collection}/:id` and `DELETE /{collection}/:id` update/delete. Creates and updates return the entity directly. Errors return `{error: string}`.
- `PUT /matches/:id` accepts pre-start metadata only (teams, stage, duration, round, notes) for administrators/editors; existing assignments are revalidated. Result and live-state fields are server owned. Score changes use referee control or result review.
- `POST /stages/:id/generate` with optional `{round: number}` generates round-robin, Swiss, or knockout matches according to the stage type; returns `{matches: Match[]}`.
- `POST /fields/:id/copy-slots` with `{source_field_id: number}` copies availability.
- `POST /matches/:id/schedule` with `{slot_id: number|null, referee_ids: number[]}` assigns a slot/referees or unschedules. The server checks field league, duration, team/referee overlaps and referee availability.
- `POST /matches/:id/review` with `{approve: boolean, notes?: string, goals1?: number, goals2?: number}` approves/publishes or returns the submitted result for correction.
- `POST /matches/:id/publish` with `{published: boolean}` controls schedule visibility. Only approved results are public.

## Entities

- Tournament: `id,name,abbrev,start_date,end_date,timezone` (dates YYYY-MM-DD; IANA timezone).
- League: `id,tournament_id,parent_id` (nullable for main leagues), `name,abbrev,mode` (`full` or `scheduling`), `referee_mode,inspections_enabled` (defaults to true, including existing leagues), `robot_count,max_weight_g` (0 means unlimited), `ball_zone_cm,voltage,size_cm,motor_limit,half_seconds,halftime_seconds`.
  The setup UI creates main leagues before sub-leagues. Main-league forms edit identity and management/referee controls; sub-league forms require an existing main league and edit inspection/clock settings. Children inherit `mode` and `referee_mode`; updates to the main league preserve child inspection and timer settings. Existing records remain compatible with the shared entity representation.
  `inspections_enabled` belongs to each sub-league and is not inherited. When false, physical inspection limits are retained and referee setup skips the checklist. Clock inputs are disabled when inherited `referee_mode` is false; stored durations are retained. With referee mode off, the API rejects live match-control commands.
- Stage: `id,tournament_id,league_id,name,type` (`round_robin,swiss,knockout,manual`), `sort_order,source_stage_id` (nullable), `round,team_limit` (0 means all teams; otherwise at least 2).
- Field: `id,tournament_id,league_id,name`.
- Slot: `id,tournament_id,field_id,starts_at,ends_at` (RFC3339 instants).
- Team: `id,tournament_id,league_id,name,affiliation,startnumber,external_key`.
- Referee: `id,tournament_id,name,user_id,available_from,available_until` (nullable RFC3339, optional availability bounds).
- Match: `id,tournament_id,league_id,stage_id,number,round,team1_id,team2_id` (nullable for a bye or scheduling-only solo run), `is_bye,field_id,slot_id,scheduled_at` (nullable), `duration_minutes,referee_ids` (array), `goals1,goals2,status` (`draft,scheduled,in_progress,submitted,approved`), `published,notes,live_state,updated_at`.

## Referee API

- `GET /matches/:id` returns a Match.
- `POST /matches/:id/control` accepts `{action, ...}` and returns the updated Match.
- Actions: `checks` with `{checks: RobotCheck[]}`; `setup` with `{blue_team_id,kickoff_team_id}`; `start`, `pause`, `advance`; `goal` with `{team_id,delta: 1|-1}`; `timeout` with `{team_id,robot: number}` (1-based); `submit` with `{signatures: Signature[],notes}`.
- `live_state` is an object: `{checks: RobotCheck[],phase: 'pregame'|'ready'|'first_half'|'halftime'|'second_half'|'finished',remaining_seconds: number,running: boolean,started_at: string|null,blue_team_id: number|null,kickoff_team_id: number|null,timeouts: {team_id,robot,ends_at}[],signatures: Signature[],signed_goals1?:number,signed_goals2?:number}`. `remaining_seconds` is the timer at `started_at`; clients derive elapsed time while running, using the `X-Server-Time` response header to correct device clock skew. `advance` only succeeds after expiry: first_half -> running halftime, halftime -> paused second_half (switches sides/kickoff), second_half -> finished. `start` starts a ready/paused phase. Pregame setup requires valid checks for all robots when `inspections_enabled` is true. Otherwise checks are skipped, while sides and kickoff setup remain required. Changing the inspection flag resets only that league’s unstarted matches to pregame, retaining schedules and referee assignments. Kickoff requires at least one assigned referee. `submit` requires finished phase and signatures for both teams and all assigned referees, and snapshots the signed score. Returning a report for correction clears signatures for renewed acceptance.
- RobotCheck: `{team_id,robot,weight_g,ball_zone,voltage,size,motors}`. Signature: `{kind:'team'|'referee',id:number,name:string,signature:string}` (signature is a drawn PNG data URI).

## Public API

- `GET /api/public/tournaments` lists tournaments.
- `GET /api/public/:abbrev` returns the same workspace shape, restricted to published schedules, approved scores and public live timer/score; excludes checks, signatures, referee user IDs and private notes.
- Catigoal-compatible `GET /rest/v1/:abbrev/leagues`, `/matches?league=…&league_stage=…`, `/standings?league=…&league_stage=…` return live Catigoal object envelopes compatible with the adjacent RCSoccerStats application. Use `format=openapi` for array responses matching the supplied specification. Stage indices are zero based within league sort order. Only published schedules and approved final results appear.

## Frontend routes

`/`, `/tournaments/:id/setup`, `/tournaments/:id/schedule`, `/tournaments/:id/review`, `/referee`, `/referee/:id`, `/public/:abbrev`, `/display/:abbrev`. Vue uses hash history, so browser links include `/#/`. Shared Axios clients use `/api/v1` and `/api/public`; private views render inside AppShell, while public pages and the TV presentation run without sign-in.
