-- Rate limiting for anonymous score submissions (audit finding LB-007):
-- without it a peer can spam inserts until the free-tier project pauses,
-- taking reads down for everyone. Two independent caps, enforced by a
-- BEFORE INSERT trigger so no API path can bypass them.
--
--   per address : 10 rows / minute  — stops network-speed floods; rotating
--                 device_id does not help. The address comes from the
--                 gateway's request.headers setting: cf-connecting-ip is
--                 set by Cloudflare from the actual TCP peer and cannot be
--                 client-spoofed (falls back to sb-/x-forwarded-for).
--   per device  :  2 rows / minute  — catches accidental double-submits.
--
-- The resolved address is stored in scores.ip (peer-filled by this
-- trigger; clients cannot write it thanks to column-scoped INSERT grants)
-- so the count queries stay index-backed.

alter table public.scores add column if not exists ip inet;
create index if not exists scores_ip_idx on public.scores (ip, created_at desc);

create or replace function public.enforce_rate_limits() returns trigger
language plpgsql security definer set search_path = public as $$
declare
  hdr jsonb := coalesce(nullif(current_setting('request.headers', true), ''), '{}')::jsonb;
  raw text := coalesce(
    nullif(hdr->>'cf-connecting-ip', ''),
    btrim(split_part(hdr->>'sb-forwarded-for', ',', -1)),
    btrim(split_part(hdr->>'x-forwarded-for', ',', -1)));
begin
  -- Cast defensively: a malformed header must not break inserts.
  new.ip := case when raw ~* '^[0-9a-f.:]+$' then raw::inet else null end;

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

create trigger scores_rate_limit
  before insert on public.scores
  for each row execute function public.enforce_rate_limits();

-- Hide the infrastructure column from anonymous readers; everything the
-- clients actually read stays selectable (board.go Top uses these exact
-- columns). Direct psql/admin access is unaffected.
revoke select on public.scores from anon, authenticated;
grant select (id, name, score, device_id, created_at) on public.scores to anon, authenticated;

drop function if exists public.debug_headers();
