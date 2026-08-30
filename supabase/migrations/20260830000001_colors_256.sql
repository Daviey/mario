-- 256-color tier (2026-08-30).
--
-- The game now renders the FIXED xterm cube (SGR 38;5; colors 16-231 +
-- the gray ramp, spec-exact on every terminal) for sessions whose
-- terminal advertises 256 colors in TERM but was not proven truecolor:
-- gnome-terminal over mosh (no DA probe survives the mosh handshake,
-- xterm-256color is ambiguous), Apple Terminal (DA probe decides
-- NOT-truecolor), tmux, urxvt-256... Previously those sessions fell to
-- the base-16 palette, whose "red" is whatever the user's profile
-- says (Tango #EF2929, Solarized #DC322F — the washed-out-mario bug).
--
-- plays.colors gains the value 256.

alter table public.plays
  drop constraint if exists plays_colors_check;

alter table public.plays
  add constraint plays_colors_check check (colors in (16, 256, 24));
