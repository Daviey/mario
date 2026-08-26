-- Level column: the board shows which level a score was reached on
-- (1-based level index at game over/win). Existing rows read as level 1.

alter table public.scores
  add column if not exists level int not null default 1;

alter table public.scores drop constraint if exists scores_level_check;
alter table public.scores add constraint scores_level_check
  check (level between 1 and 99);

-- Inserts may set the new column (old clients keep the default 1).
grant insert (name, score, level, device_id, pow_nonce)
  on public.scores to anon, authenticated;

-- board_rows gains the column. The return type changes, so drop and
-- recreate (create or replace cannot change a function's return type);
-- grants die with the old function and are reissued below.
drop function if exists public.board_rows(uuid, int);
create function public.board_rows(p_device_id uuid default null, p_limit int default 50)
returns table (name text, score int, level int, mine boolean, created_at timestamptz)
language sql stable security definer set search_path = public as $$
  select s.name, s.score, s.level,
         p_device_id is not null and s.device_id = p_device_id as mine,
         s.created_at
  from public.scores s
  order by s.score desc, s.created_at asc, s.id
  limit least(greatest(coalesce(p_limit, 50), 1), 100);
$$;

grant execute on function public.board_rows(uuid, int) to anon, authenticated;
