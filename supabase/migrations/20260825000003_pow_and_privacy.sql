-- Hardening round 2 (post-audit): proof-of-work gating, device_id privacy,
-- and retention. Pairs with 20260825000001_name_charset and
-- 20260825000002_rate_limits.
--
-- 1. Proof of work: every insert must carry pow_nonce such that hex sha256
--    of "<device_id>:<score>:<nonce>" starts with "00000" (20 bits, ~1M
--    hashes on average: instant for an honest player via crypto/sha256,
--    real CPU cost per row for scripted flooding — identity rotation no
--    longer helps). Difficulty lives in board/pow.go; keep them in sync.
-- 2. device_id privacy: anon can no longer SELECT the column (it was a
--    public cross-session tracking identifier and enabled forged "mine"
--    highlighting). Mine-ness moves into the board_rows RPC below.
-- 3. Retention: nightly pg_cron job keeps the top 500 scores and drops
--    anything older than 60 days, so floods and stale rows don't pile up.

create extension if not exists pgcrypto;

alter table public.scores add column if not exists pow_nonce text;

-- pow_nonce must be client-supplied; everything else stays as before.
revoke insert on public.scores from anon, authenticated;
grant insert (name, score, device_id, pow_nonce) on public.scores to anon, authenticated;

create or replace function public.verify_pow() returns trigger
language plpgsql security definer set search_path = public, extensions as $$
declare
  payload text := new.device_id::text || ':' || new.score::text || ':' || coalesce(new.pow_nonce, '');
begin
  if position('00000' in encode(digest(payload, 'sha256'), 'hex')) <> 1 then
    raise exception 'invalid proof of work';
  end if;
  return new;
end;
$$;

drop trigger if exists scores_pow on public.scores;
create trigger scores_pow
  before insert on public.scores
  for each row execute function public.verify_pow();

-- Hide device_id from anonymous readers; the RPC re-adds mine-ness safely.
revoke select on public.scores from anon, authenticated;
grant select (id, name, score, created_at) on public.scores to anon, authenticated;

-- Read path for clients: top rows with precomputed mine flag. Security
-- definer because the caller can no longer select device_id directly.
create or replace function public.board_rows(p_device_id uuid default null, p_limit int default 50)
returns table (name text, score int, mine boolean, created_at timestamptz)
language sql stable security definer set search_path = public as $$
  select s.name, s.score,
         p_device_id is not null and s.device_id = p_device_id as mine,
         s.created_at
  from public.scores s
  order by s.score desc, s.created_at asc, s.id
  limit least(greatest(coalesce(p_limit, 50), 1), 100);
$$;

grant execute on function public.board_rows(uuid, int) to anon, authenticated;

-- Nightly retention: keep the best 500 ever seen, drop rows older than 60
-- days regardless (bounds flood persistence and table growth).
create extension if not exists pg_cron;
do $$
begin
  if not exists (select 1 from cron.job where jobname = 'scores_retention') then
    perform cron.schedule('scores_retention', '23 3 * * *', $job$
      delete from public.scores
      where created_at < now() - interval '60 days'
         or id not in (select id from public.scores order by score desc limit 500)
    $job$);
  end if;
end $$;
