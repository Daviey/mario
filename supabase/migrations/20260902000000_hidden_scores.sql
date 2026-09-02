-- Hidden rows instead of deletes: the verifier no longer deletes
-- mismatched submissions. A replay that disagrees with the row's claim
-- (or a cross-version recording) is corrected to the replayed score and
-- marked hidden when the disagreement is systematic, keeping the row AND
-- its recording for forensics; board_rows filters hidden out of every
-- board surface. Verified rows keep their score.

alter table public.scores
  add column if not exists hidden boolean not null default false;

-- board_rows v4: hide hidden rows from every board surface (the RPC is
-- the single read path — terminal and web alike). Column order and the
-- sort tiebreak are unchanged; the Go client's Row decode follows the
-- column order.
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
    and not s.hidden
  order by s.score desc, s.verified desc, s.created_at asc, s.id
  limit least(greatest(coalesce(p_limit, 50), 1), 100)
$$;

grant execute on function public.board_rows(uuid, int, text, date)
  to anon, authenticated;

-- The nightly retention job (pow_and_privacy migration) already prunes
-- the top-500 tail; hidden rows age out on the same schedule.
