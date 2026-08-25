-- Tighten the name constraint to match the client's documented A-Z0-9 .- charset
-- and restrict which columns anon may insert to prevent id/created_at spoofing.

alter table public.scores drop constraint scores_name_check;
alter table public.scores add constraint scores_name_check check (name ~ '^[A-Z0-9 .-]{1,8}$');

revoke insert on public.scores from anon, authenticated;
grant insert (name, score, device_id) on public.scores to anon, authenticated;
