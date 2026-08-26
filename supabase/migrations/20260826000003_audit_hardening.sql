-- Post-audit hardening round 3 (2026-08-26). Four independent fixes:
--
-- 1. Retention cron was mode-blind and verification-blind: the keep-set
--    was the GLOBAL top-500 by raw score, so (a) junk rows (unverified,
--    replay-carrying, deleted by the verifier within 15 min) could evict
--    genuine rows if the nightly run landed first, and (b) daily rows
--    outside the global top-500 were wiped every night — past-day boards
--    effectively did not survive. New predicate:
--      * age purge unchanged (60 days, everything)
--      * unverified rows that CARRY a replay (i.e. produced by the
--        submission flow, not legacy) are dropped after 24 h
--      * classic keep-set: top 500 among verified-or-legacy rows only —
--        junk can no longer occupy retention slots
--      * daily keep-set: top 100 per challenge day among
--        verified-or-legacy rows
-- 2. enforce_rate_limits(): the inet cast ran inside a CASE expression,
--        where a regex-passing but unparseable value RAISES instead of
--        degrading to null — a single malformed forwarded header could
--        break inserts globally. The cast is now exception-wrapped.
-- 3. scores_name_check used collation-dependent ranges: under a non-C
--    collation accented Latin letters can satisfy [A-Z], so the DB
--    constraint is weaker than the client's byte whitelist. The CHECK
--    now pins COLLATE "C".
-- 4. engine_version had no length bound (asymmetric with the replay
--    size CHECK); capped at 32 chars. It is only ever compared against
--    board.EngineVersion.

-- 1. Retention: reschedule with the mode-aware, verification-aware job.
select cron.unschedule('scores_retention');
select cron.schedule('scores_retention', '23 3 * * *', $job$
  delete from public.scores
  where created_at < now() - interval '60 days'
     or (verified = false and replay is not null
         and created_at < now() - interval '24 hours')
     or (mode = 'classic' and id not in (
           select id from public.scores
           where mode = 'classic' and (verified or replay is null)
           order by score desc, created_at asc, id limit 500))
     or (mode = 'daily' and id not in (
           select id from (
             select id, row_number() over (
               partition by day order by score desc, created_at asc, id) as rn
             from public.scores where mode = 'daily'
               and (verified or replay is null)
           ) ranked where ranked.rn <= 100))
$job$);

-- 2. Rate-limit trigger: exception-wrapped address resolution.
create or replace function public.enforce_rate_limits() returns trigger
language plpgsql security definer set search_path = public as $$
declare
  hdr jsonb := coalesce(nullif(current_setting('request.headers', true), ''), '{}')::jsonb;
  raw text := coalesce(
    nullif(hdr->>'cf-connecting-ip', ''),
    btrim(split_part(hdr->>'sb-forwarded-for', ',', -1)),
    btrim(split_part(hdr->>'x-forwarded-for', ',', -1)));
begin
  -- A malformed header must degrade to null (per-IP cap silently off),
  -- never break the insert: cast inside a nested BEGIN/EXCEPTION block.
  new.ip := null;
  begin
    if raw ~* '^[0-9a-f.:]+$' then
      new.ip := raw::inet;
    end if;
  exception when others then
    new.ip := null;
  end;

  if new.ip is not null and (
    select count(*) from public.scores
    where ip = new.ip and created_at > now() - interval '1 minute'
  ) >= 10 then
    raise exception 'too many submissions from your network, slow down';
  end if;

  if (
    select count(*) from public.scores
    where device_id = new.device_id and created_at > now() - interval '1 minute'
  ) >= 2 then
    raise exception 'too many submissions, slow down';
  end if;

  return new;
end;
$$;

-- 3. Name CHECK pinned to C collation (byte-exact ASCII ranges).
alter table public.scores drop constraint scores_name_check;
alter table public.scores add constraint scores_name_check
  check ((name collate "C") ~ '^[A-Z0-9 .-]{1,8}$');

-- 4. engine_version length bound.
alter table public.scores drop constraint if exists scores_engine_version_len;
alter table public.scores add constraint scores_engine_version_len
  check (char_length(engine_version) <= 32);
