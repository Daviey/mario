// Worktree policy guard (dave, 2026-08-25).
//
// Standing policy: agent file edits never land in the direct checkout
// /home/dave/dev/game. Build every change in a linked worktree —
//   .worktrees/<feature>   (in-repo, preferred)
//   ../game-<feature>      (sibling layout, also sanctioned)
// — commit there, then `git merge <branch>` from the main checkout.
// The main checkout is only ever touched by git metadata operations.
//
// Guarded tools: edit, write (bash is deliberately unguarded so git
// merge/push/rebase still run in the main checkout).
//
// This guard FAILS OPEN: unknown input shapes and internal errors allow the
// call through. A guard bug must not brick every edit/write session; the
// worst case here is a missed policy hit, not corruption.

import type { HookAPI } from "@oh-my-pi/pi-coding-agent/extensibility/hooks";

const GUARDED_ROOT = "/home/dave/dev/game";
const SANDBOX_PREFIX = GUARDED_ROOT + "/.worktrees/";

const BLOCKED_TOOLS: Record<string, true> = { edit: true, write: true };

const POLICY_REASON =
  "worktree policy: file edits never land in the direct checkout /home/dave/dev/game. " +
  "Build the change in a linked worktree (.worktrees/<feature> in-repo, or sibling " +
  "../game-<feature>), commit there, then `git merge <branch>` from the main checkout " +
  "(metadata only; bash is allowed there).";

/** Runtime-narrowed string field read — no unchecked casts, tolerant of any
 *  input shape the hook API hands us (fail-open on absence). */
function fieldString(obj: unknown, key: string): string | undefined {
  if (obj === null || typeof obj !== "object" || !(key in obj)) return undefined;
  const v = Reflect.get(obj, key);
  return typeof v === "string" ? v : undefined;
}

/** Lexically resolve a tool path against cwd. Returns undefined when the
 *  target is not a judgeable filesystem path (internal URLs, ~-relative). */
function resolveTarget(cwd: string, p: string): string | undefined {
  if (p.includes("://")) return undefined; // xd:// local:// memory:// ...
  if (p.startsWith("~")) return undefined; // home-relative, outside this policy
  const base = p.startsWith("/") ? p : cwd.replace(/\/+$/, "") + "/" + p;
  const out: string[] = [];
  for (const seg of base.split("/")) {
    if (seg === "" || seg === ".") continue;
    if (seg === "..") {
      out.pop();
      continue;
    }
    out.push(seg);
  }
  return "/" + out.join("/");
}

/** True when an absolute path is inside the guarded repo but outside the
 *  sanctioned .worktrees/ sandbox. Sibling ../game-* worktrees are outside
 *  GUARDED_ROOT entirely and therefore never guarded. Archive/SQLite
 *  selectors ("x.zip:inner", "db.sqlite:table") keep their base path inside
 *  the guard because the container file itself is the mutation. */
function inGuardedArea(abs: string): boolean {
  if (abs !== GUARDED_ROOT && !abs.startsWith(GUARDED_ROOT + "/")) return false;
  return !abs.startsWith(SANDBOX_PREFIX);
}
/** Extract every filesystem target from an edit tool `input` payload:
 *  [PATH#TAG] / [PATH] section headers and MV DEST move lines. */
function editTargets(input: string): string[] {
  const targets: string[] = [];
  let m: RegExpExecArray | null;
  const section = /^\[([^\]\n]+?)(?:#[0-9A-Fa-f]{1,8})?\]/gm;
  while ((m = section.exec(input))) targets.push(m[1]);
  const mv = /^MV[ \t]+["']?([^"'\n]+)["']?[ \t]*$/gm;
  while ((m = mv.exec(input))) targets.push(m[1]);
  return targets;
}

/** Strip an optional copied [path#TAG] wrapper from a write path. */
function stripWriteWrapper(p: string): string {
  const m = /^\[([^\]\n]+?)\]/.exec(p);
  return m ? m[1] : p;
}

export default function worktreeGuard(pi: HookAPI): void {
  pi.on("tool_call", async (event, ctx) => {
    try {
      const tool = fieldString(event, "toolName") ?? "";
      if (!BLOCKED_TOOLS[tool]) return;

      const cwd = fieldString(ctx, "cwd") || (typeof process !== "undefined" && process.cwd ? process.cwd() : "");

      const raw: string[] = [];

      const input = Reflect.get(event, "input");
      if (tool === "write") {
        const p = fieldString(input, "path");
        if (p !== undefined) raw.push(stripWriteWrapper(p));
      } else {
        const s = fieldString(input, "input");
        if (s !== undefined) raw.push(...editTargets(s));
      }

      for (const r of raw) {
        const abs = resolveTarget(cwd, r);
        if (abs !== undefined && inGuardedArea(abs)) {
          return { block: true, reason: `${POLICY_REASON} Refused target: ${r}` };
        }
      }
      return; // nothing judgeable, or all targets sanctioned
    } catch {
      return; // fail open — never brick edit/write on a guard bug
    }
  });
}
