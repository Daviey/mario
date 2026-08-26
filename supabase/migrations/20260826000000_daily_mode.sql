-- Daily challenge mode. Scores carry a mode ('classic' | 'daily') and,
-- for daily rows, the challenge day (UTC). The classic board is unchanged;
-- daily boards are per-day.

alter table public.scores
  add column if not exists mode text not null default 'classic';

alter table public.scores
  add column if not exists day date;

do $$
begin
  if not exists (select 1 from pg_constraint where conname = 'scores_mode_chk') then
    alter table public.scores
      add constraint scores_mode_chk check (mode in ('classic','daily')) not valid;
  end if;
end $$;

-- Column grants follow the pow_and_privacy split: anon inserts may carry
-- the new columns, anon reads may see them (day+mode are public facts).
revoke insert on public.scores from anon, authenticated;
grant insert (name, score, device_id, pow_nonce, mode, day)
  on public.scores to anon, authenticated;

revoke select on public.scores from anon, authenticated;
grant select (id, name, score, created_at, mode, day)
  on public.scores to anon, authenticated;

-- Per-day leaderboard lookups.
create index if not exists scores_daily_idx
  on public.scores (day, score desc) where mode = 'daily';

-- board_rows gains mode/day filters. Replacing (not overloading) the
-- function keeps one resolved signature. p_mode null (older clients) or
-- 'classic' reads the classic board; 'daily' reads a day's challenge
-- board.
drop function if exists public.board_rows(uuid, int);

create or replace function public.board_rows(
  p_device_id uuid default null,
  p_limit int default 50,
  p_mode text default null,
  p_day date default null
)
returns table (name text, score int, mine boolean, created_at timestamptz)
language sql stable security definer set search_path = public as $$
  select s.name, s.score,
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
