-- High score board: anon clients insert unverified rows; only the
-- service_role key (GitHub Action verifier) can flip verified or delete.
create table public.scores (
  id uuid default gen_random_uuid() primary key,
  name text not null check (char_length(name) between 1 and 8),
  score int not null check (score >= 0 and score < 10000000),
  seed bigint not null default 0,
  replay jsonb not null
    -- Bounded storage: object wire format {"v":1,"i":[...]}; 1 MiB covers
    -- the client's 30-minute recording cap (~750 KB worst case).
    check (jsonb_typeof(replay) = 'object'
       and jsonb_typeof(replay -> 'i') = 'array'
       and octet_length(replay::text) <= 1048576),
  engine_version text not null,
  device_id uuid not null,
  verified bool not null default false,
  created_at timestamptz default now()
);

create index scores_leaderboard_idx on public.scores (verified, score desc);
create index scores_device_idx on public.scores (device_id, created_at desc);

alter table public.scores enable row level security;

create policy "anon can insert unverified rows only"
  on public.scores for insert
  with check (not verified);

create policy "anyone can read verified rows"
  on public.scores for select
  using (verified);

-- Auto-expose is disabled, so grants are explicit:
grant insert, select on public.scores to anon, authenticated;
grant all on public.scores to service_role;

-- Rate limit: anonymous inserts on a public site need a per-device cap.
create function public.scores_rate_limit() returns trigger
language plpgsql security definer set search_path = public as $$
declare n int;
begin
  select count(*) into n from public.scores
   where device_id = new.device_id and created_at > now() - interval '1 hour';
  if n >= 5 then
    raise exception 'rate limit: max 5 submissions per device per hour';
  end if;
  return new;
end $$;

create trigger scores_rate_limit_trg before insert on public.scores
for each row execute function public.scores_rate_limit();
