export interface Tournament {
  id: number
  name: string
  abbrev: string
  start_date: string
  end_date: string
  timezone: string
}
export interface League {
  id: number
  tournament_id: number
  parent_id: number | null
  name: string
  abbrev: string
  mode: 'full' | 'scheduling'
  referee_mode: boolean
  inspections_enabled: boolean
  standings_auto_publish: boolean
  robot_count: number
  max_weight_g: number
  ball_zone_cm: number
  voltage: number
  size_cm: number
  motor_limit: number
  half_seconds: number
  halftime_seconds: number
}
export interface Stage {
  id: number
  tournament_id: number
  league_id: number
  name: string
  type: 'round_robin' | 'swiss' | 'knockout' | 'individual' | 'manual'
  sort_order: number
  team_limit: number
  expected_teams: number
  source_stage_id: number | null
  round: number
}
export interface Field {
  id: number
  tournament_id: number
  league_id: number
  name: string
}
export interface Slot {
  id: number
  tournament_id: number
  field_id: number
  starts_at: string
  ends_at: string
}
export interface Team {
  id: number
  tournament_id: number
  league_id: number
  name: string
  affiliation: string
  startnumber: number
  external_key: string
}
export interface Referee {
  id: number
  tournament_id: number
  name: string
  user_id: string
  available_from: string | null
  available_until: string | null
}
export interface RobotCheck {
  team_id: number
  robot: number
  weight_g: number
  ball_zone: boolean
  voltage: boolean
  size: boolean
  motors: boolean
}
export interface Signature {
  kind: 'team' | 'referee'
  id: number
  name: string
  signature: string
}
export interface RobotTimeout {
  team_id: number
  robot: number
  ends_at: string
}
export interface MatchEvent {
  id: number
  at: string
  type: string
  phase: string
  remaining_seconds: number
  team_id?: number
  robot?: number
  side?: 'blue' | 'yellow'
  next_phase?: string
  forced?: boolean
}
export interface LiveState {
  events?: MatchEvent[]
  ended_early?: boolean
  end_reason?: string
  checks: RobotCheck[]
  phase: 'pregame' | 'ready' | 'first_half' | 'halftime' | 'second_half' | 'finished'
  remaining_seconds: number
  running: boolean
  started_at: string | null
  blue_team_id: number | null
  kickoff_team_id: number | null
  timeouts: RobotTimeout[]
  signatures: Signature[]
  signed_goals1?: number
  signed_goals2?: number
}
export interface Match {
  is_placeholder: boolean
  is_bye: boolean
  id: number
  tournament_id: number
  league_id: number
  stage_id: number
  number: number
  round: number
  team1_id: number
  team2_id: number | null
  field_id: number | null
  slot_id: number | null
  scheduled_at: string | null
  duration_minutes: number
  referee_ids: number[]
  goals1: number
  goals2: number
  status: 'draft' | 'scheduled' | 'in_progress' | 'submitted' | 'approved'
  published: boolean
  notes: string
  live_state: LiveState | null
  updated_at: string
}
export interface Standing {
  league_id: number
  stage_id: number
  team_id: number
  played: number
  wins: number
  draws: number
  losses: number
  scored: number
  conceded: number
  difference: number
  points: number
  rank: number
}
export interface PublishedStandings {
  league_id: number
  stage_id: number
  published_at: string
  rows: Standing[]
}
export interface Workspace {
  tournament: Tournament
  leagues: League[]
  stages: Stage[]
  fields: Field[]
  slots: Slot[]
  teams: Team[]
  referees: Referee[]
  matches: Match[]
  standings: Standing[]
  published_standings: PublishedStandings[]
}
