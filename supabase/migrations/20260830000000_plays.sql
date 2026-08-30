-- Server-side play telemetry: one row per SSH game session, written by
-- the game server itself at disconnect (cmd/mario serve.go). This table
-- is WRITE-ONLY for API keys: the game inserts and never reads, and anon
-- has no select grant and no select policy at all — analytics stay a
-- direct-DB/operator concern, exactly like scores' private columns.
--
-- Privacy line: rows originate only from OUR servers (the SSH host
-- logging its own sessions). Player machines never phone home — the
-- no-persistence/no-tracking rule is untouched; local and web play stay
-- invisible unless the player submits a score.
--
-- Anti-abuse mirrors the scores hardening: the publishable key is
-- public, so anyone can attempt inserts — a BEFORE INSERT trigger caps
-- them (per supplied ip 30/min, global 120/min) and a nightly pg_cron
-- job prunes rows older than a year. Rows are ~200 bytes; bounded junk
-- is the accepted cost of a keyless write path.

create table public.plays (
  id uuid default gen_random_uuid() primary key,
  ip text check (ip is null or ip ~ '^[0-9a-fA-F.:]+$'),
  started_at timestamptz not null,
  ended_at timestamptz not null check (ended_at >= started_at),
  level int not null default 1 check (level between 1 and 99),
  score int not null default 0 check (score between 0 and 9999999),
  submitted boolean not null default false,
  runs int not null default 0 check (runs between 0 and 1000000),
  term text check (char_length(term) <= 64),
  colorterm text check (char_length(colorterm) <= 32),
  colors int not null default 16 check (colors in (16, 24)),
  client text check (char_length(client) <= 128),
  input_regime text check (input_regime is null or input_regime in ('kitty', 'legacy')),
  viewport text check (viewport is null or viewport ~ '^[0-9]+x[0-9]+$'),
  engine_version text check (char_length(engine_version) <= 32),
  created_at timestamptz default now()
);

create index plays_created_idx on public.plays (created_at desc);
create index plays_ip_idx on public.plays (ip, created_at desc);

alter table public.plays enable row level security;

-- Insert-only. No select policy exists, so RLS denies every read for
-- anon/authenticated even if a grant appears later — the write-only
-- contract is enforced twice.
create policy "anon can insert"
  on public.plays for insert
  with check (true);

grant insert on public.plays to anon, authenticated;

-- Rate limits: per supplied ip (the player's SSH origin, client-filled
-- because the TCP peer of the insert is our own server) and one global
-- cap as the backstop against rotating fake ips.
create or replace function public.enforce_play_limits() returns trigger
language plpgsql security definer set search_path = public as $$
begin
  if new.ip is not null and (
    select count(*) from public.plays
    where ip = new.ip and created_at > now() - interval '1 minute'
  ) >= 30 then
    raise exception 'too many play rows, slow down';
  end if;
  if (
    select count(*) from public.plays
    where created_at > now() - interval '1 minute'
  ) >= 120 then
    raise exception 'too many play rows, slow down';
  end if;
  return new;
end;
$$;

create trigger plays_rate_limit
  before insert on public.plays
  for each row execute function public.enforce_play_limits();

-- Retention: keep a year of session telemetry, prune nightly.
do $$
begin
  if not exists (select 1 from cron.job where jobname = 'plays_retention') then
    perform cron.schedule('plays_retention', '41 3 * * *', $job$
      delete from public.plays
      where created_at < now() - interval '365 days'
    $job$);
  end if;
end $$;
