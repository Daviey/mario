-- Replay-verified scores: every submission carries the run's input log
-- (deterministic engine → replaying it must reproduce the exact score).
-- A GitHub Action verifier (service key) replays pending rows, marks the
-- survivors verified and deletes the rest. Legacy rows predate replays
-- and simply stay unverified.
-- replay is the wire-format STRING (see the replay package), not jsonb:
-- PostgREST then round-trips it verbatim into the verifier's string field.
do $$ begin
  if exists (select 1 from information_schema.columns
             where table_schema = 'public' and table_name = 'scores'
               and column_name = 'replay' and data_type = 'jsonb') then
    alter table public.scores alter column replay type text using replay::text;
  end if;
end $$;

alter table public.scores
  add column if not exists replay text,
  add column if not exists engine_version text,
  add column if not exists verified boolean not null default false;

alter table public.scores drop constraint if exists scores_replay_size;
alter table public.scores add constraint scores_replay_size
  check (replay is null or char_length(replay) <= 262144);

-- New rows must carry a replay; the client refuses to submit without one
-- and the server enforces the same contract.
create or replace function public.require_replay() returns trigger
language plpgsql as $$
begin
  if new.replay is null then
    raise exception 'replay required: submissions must carry an input log';
  end if;
  return new;
end $$;

drop trigger if exists trg_replay_required on public.scores;
create trigger trg_replay_required
  before insert on public.scores
  for each row execute function public.require_replay();

-- Inserts may now also set the verification columns.
revoke insert on public.scores from anon, authenticated;
grant insert (name, score, level, device_id, pow_nonce, mode, day, replay, engine_version)
  on public.scores to anon, authenticated;

-- The verifier (service role) needs full table access.
grant select, update, delete on public.scores to service_role;

-- Pending queue scan support.
create index if not exists scores_pending_idx
  on public.scores (created_at)
  where verified = false and replay is not null;

-- board_rows v3: the verified flag rides along (before created_at; the Go
-- client's Row decode follows this column order). The drop handles fresh
-- databases replaying this migration after the 20260826000001 union.
drop function if exists public.board_rows(uuid, int, text, date);
create or replace function public.board_rows(
  p_device_id uuid default null,
  p_limit int default 50,
  p_mode text default null,
  p_day date default null
)
returns table (name text, score int, level int, mine boolean, verified boolean, created_at timestamptz)
language sql stable security definer set search_path = public as $$
  select s.name, s.score, s.level,
         p_device_id is not null and s.device_id = p_device_id as mine,
         s.verified,
         s.created_at
  from public.scores s
  where s.mode = coalesce(p_mode, 'classic')
    and (p_day is null or s.day = p_day)
  order by s.score desc, s.verified desc, s.created_at asc, s.id
  limit least(greatest(coalesce(p_limit, 50), 1), 100)
$$;

grant execute on function public.board_rows(uuid, int, text, date)
  to anon, authenticated;
