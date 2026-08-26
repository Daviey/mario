-- Union of the two 2026-08-26 board changes: the level column
-- (20260826000000_level.sql) and the daily challenge mode
-- (20260826000000_daily_mode.sql). Both redefined board_rows; on a fresh
-- database the alphabetical apply order would leave only the level
-- variant (no mode/day filter). This migration is the single source of
-- truth for the RPC: it returns level AND filters by mode/day.

alter table public.scores
  add column if not exists level int not null default 1;

alter table public.scores drop constraint if exists scores_level_check;
alter table public.scores add constraint scores_level_check
  check (level between 1 and 99);

do $$
begin
  if not exists (select 1 from pg_constraint where conname = 'scores_mode_chk') then
    alter table public.scores
      add constraint scores_mode_chk check (mode in ('classic','daily')) not valid;
  end if;
end $$;

-- Inserts may set every client-supplied column; reads may see the public ones.
revoke insert on public.scores from anon, authenticated;
grant insert (name, score, level, device_id, pow_nonce, mode, day)
  on public.scores to anon, authenticated;

revoke select on public.scores from anon, authenticated;
grant select (id, name, score, created_at, level, mode, day)
  on public.scores to anon, authenticated;

drop function if exists public.board_rows(uuid, int);
drop function if exists public.board_rows(uuid, int, text, date);

create function public.board_rows(
  p_device_id uuid default null,
  p_limit int default 50,
  p_mode text default null,
  p_day date default null
)
returns table (name text, score int, level int, mine boolean, created_at timestamptz)
language sql stable security definer set search_path = public as $$
  select s.name, s.score, s.level,
         p_device_id is not null and s.device_id = p_device_id as mine,
         s.created_at
  from public.scores s
  where s.mode = coalesce(p_mode, 'classic')
    and (p_day is null or s.day = p_day)
  order by s.score desc, s.created_at asc, s.id
  limit least(greatest(coalesce(p_limit, 50), 1), 100)
$$;

grant execute on function public.board_rows(uuid, int, text, date)
  to anon, authenticated;
