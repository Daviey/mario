-- Play context: operator-only diagnostics on every score row — where and
-- how the run was played. Pairs with board.Entry's context fields
-- (board/board.go ClampPlayContext mirrors these CHECKs client-side).
--
-- Columns are client-supplied (surface/term/colorterm/input_regime/
-- viewport; user_agent when the client knows it — the web build sends the
-- browser's UA) with the HTTP User-Agent header as the server-side
-- default, so even a legacy client row gets a meaningful user_agent.
-- NONE of these are exposed to anon: no select grant, and board_rows does
-- not return them (operator visibility only, via the service key used by
-- -verify-pending).

alter table public.scores
  add column if not exists surface text,
  add column if not exists user_agent text,
  add column if not exists term text,
  add column if not exists colorterm text,
  add column if not exists input_regime text,
  add column if not exists viewport text;

do $$
begin
  if not exists (select 1 from pg_constraint where conname = 'scores_surface_chk') then
    alter table public.scores add constraint scores_surface_chk
      check (surface is null or surface in ('local', 'ssh', 'web')) not valid;
  end if;
  if not exists (select 1 from pg_constraint where conname = 'scores_user_agent_len_chk') then
    alter table public.scores add constraint scores_user_agent_len_chk
      check (char_length(coalesce(user_agent, '')) <= 256) not valid;
  end if;
  if not exists (select 1 from pg_constraint where conname = 'scores_term_len_chk') then
    alter table public.scores add constraint scores_term_len_chk
      check (char_length(coalesce(term, '')) <= 64
         and char_length(coalesce(colorterm, '')) <= 32) not valid;
  end if;
  if not exists (select 1 from pg_constraint where conname = 'scores_input_regime_chk') then
    alter table public.scores add constraint scores_input_regime_chk
      check (input_regime is null or input_regime in ('kitty', 'legacy')) not valid;
  end if;
  if not exists (select 1 from pg_constraint where conname = 'scores_viewport_chk') then
    alter table public.scores add constraint scores_viewport_chk
      check (viewport is null or viewport ~ '^[0-9]+x[0-9]+$') not valid;
  end if;
end $$;

-- The insert grant is column-scoped: re-grant it with the new columns
-- included, or every submission carrying play context is rejected.
revoke insert on public.scores from anon, authenticated;
grant insert (name, score, level, device_id, pow_nonce, mode, day, replay,
              engine_version, surface, user_agent, term, colorterm,
              input_regime, viewport)
  on public.scores to anon, authenticated;

-- No select grant on the new columns: they stay operator-only, like ip.

-- Default user_agent from the request's User-Agent header when the client
-- did not send one (the native CLI always sends a real UA string; this
-- covers legacy/other clients).
create or replace function public.default_user_agent() returns trigger
language plpgsql as $$
declare
  hdr json;
begin
  if coalesce(new.user_agent, '') = '' then
    hdr := null::json;
    begin
      hdr := current_setting('request.headers', true)::json;
    exception when others then
      hdr := null::json;
    end;
    if hdr is not null then
      new.user_agent := left(coalesce(hdr->>'user-agent', ''), 256);
    end if;
  end if;
  return new;
end;
$$;

drop trigger if exists scores_user_agent on public.scores;
create trigger scores_user_agent
  before insert on public.scores
  for each row execute function public.default_user_agent();
