-- High score board: anonymous clients insert rows directly; everyone can
-- read. No verification layer — scores are client-attested (see README-less
-- reality: friends board). device_id supports future rate limiting/dedup.
create table public.scores (
  id uuid default gen_random_uuid() primary key,
  name text not null check (char_length(name) between 1 and 8),
  score int not null check (score >= 0 and score < 10000000),
  device_id uuid not null,
  created_at timestamptz default now()
);

create index scores_leaderboard_idx on public.scores (score desc);
create index scores_device_idx on public.scores (device_id, created_at desc);

alter table public.scores enable row level security;

create policy "anon can insert"
  on public.scores for insert
  with check (true);

create policy "anyone can read"
  on public.scores for select
  using (true);

-- Auto-expose is disabled, so grants are explicit:
grant insert, select on public.scores to anon, authenticated;
