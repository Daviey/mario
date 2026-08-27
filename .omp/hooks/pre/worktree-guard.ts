// Worktree policy guard (dave, 2026-08-25; generalized 2026-08-27).
//
// Standing policy: agent file edits never land in the direct checkout of the
// git repo this hook lives in. Build every change in a linked worktree —
//   <repo>/.worktrees/<feature>   (in-repo, preferred)
//   ../<repo>-<feature>           (sibling layout, also sanctioned)
// — commit there, then `git merge <branch>` from the main checkout.
// The main checkout is only ever touched by git metadata operations.
//
// The guarded root is discovered at runtime, never hardcoded: walk up from
// this file's own location for `.git`. A `.git` directory marks the main
// checkout; a `.git` file is a linked-worktree pointer whose `gitdir:` line
// leads back to the main checkout. So this file drops into any repo's
// .omp/hooks/pre/ and guards that repo, and a session inside a linked
// worktree (which loads the worktree's own copy of the hook) resolves the
// same root. Sibling worktrees sit outside the root and are never guarded.
// A copy not inside any git repo (e.g. user-level ~/.omp/agent/) finds no
// root and fails open.
//
// Guarded tools: edit, write (bash is deliberately unguarded so git
// merge/push/rebase still run in the main checkout).
//
// This guard FAILS OPEN: unknown input shapes, unattributable locations and
// internal errors allow the call through. A guard bug must not brick every
// edit/write session; the worst case here is a missed policy hit, not
// corruption. Known bypasses (accepted): bash-file-writes (sed/tee),
// conflict:// writes, lsp rename_file, xd://ast_edit device dispatch.

import { readFileSync, statSync } from "node:fs";
import { fileURLToPath } from "node:url";
import type { HookAPI } from "@oh-my-pi/pi-coding-agent/extensibility/hooks";

/** Directory containing this hook file (<repo>/.omp/hooks/pre/...). */
const HOOK_DIR = fileURLToPath(new URL(".", import.meta.url));

const BLOCKED_TOOLS: Record<string, true> = { edit: true, write: true };

/** Lexically normalize an absolute POSIX path (collapse //, . and ..). */
function normalizeAbs(p: string): string {
  const out: string[] = [];
  for (const seg of p.split("/")) {
    if (seg === "" || seg === ".") continue;
    if (seg === "..") {
      out.pop();
      continue;
    }
    out.push(seg);
  }
  return "/" + out.join("/");
}

/** Tolerant filesystem probe shared by every stat in the root walk:
 *  missing/unreadable paths read as undefined, never throw. */
function statKind(p: string): "dir" | "file" | undefined {
  try {
    const st = statSync(p);
    if (st.isDirectory()) return "dir";
    if (st.isFile()) return "file";
  } catch {
    return undefined;
  }
  return undefined;
}

/**
 * Main-checkout root of the repo containing this hook, or undefined when no
 * `.git` can be attributed. Walk up from the hook file's directory: `.git`
 * as a directory is the main checkout; `.git` as a file is a linked worktree
 * whose `gitdir: <main>/.git/worktrees/<name>` pointer resolves back to the
 * main checkout (which must itself still exist — sanity against odd or
 * forged pointers; anything unparseable fails open).
 */
function findGuardedRoot(): string | undefined {
  let dir = HOOK_DIR.replace(/\/+$/, "");
  for (;;) {
    const dotGit = dir + "/.git";
    const kind = statKind(dotGit);
    if (kind === "dir") return dir;
    if (kind === "file") {
      let text = "";
      try {
        text = readFileSync(dotGit, "utf8");
      } catch {
        text = "";
      }
      const m = /^gitdir:\s*(\S.*?)\s*$/.exec(text);
      if (m) {
        const raw = m[1].startsWith("/") ? m[1] : dir + "/" + m[1];
        const gitdir = normalizeAbs(raw);
        const wt = /^(.*)\/\.git\/worktrees\/[^/]+$/.exec(gitdir);
        if (wt && statKind(wt[1] + "/.git") === "dir") return wt[1];
      }
      return undefined;
    }
    const cut = dir.lastIndexOf("/");
    if (cut <= 0) return undefined;
    dir = dir.slice(0, cut);
  }
}

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
  return normalizeAbs(base);
}

/** True when an absolute path is inside the guarded repo but outside the
 *  sanctioned .worktrees/ sandbox. Sibling worktrees are outside the guarded
 *  root entirely and therefore never guarded. Archive/SQLite selectors
 *  ("x.zip:inner", "db.sqlite:table") keep their base path inside the guard
 *  because the container file itself is the mutation. */
function inGuardedArea(abs: string, root: string): boolean {
  if (abs !== root && !abs.startsWith(root + "/")) return false;
  return !abs.startsWith(root + "/.worktrees/");
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

      const guardedRoot = findGuardedRoot();
      if (guardedRoot === undefined) return; // hook copy not inside a git repo — fail open

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
        if (abs !== undefined && inGuardedArea(abs, guardedRoot)) {
          return {
            block: true,
            reason:
              "worktree policy: file edits never land in the direct checkout " +
              guardedRoot +
              ". Build the change in a linked worktree (.worktrees/<feature> in-repo, or a sibling " +
              "worktree), commit there, then `git merge <branch>` from the main checkout " +
              "(metadata only; bash is allowed there). Refused target: " +
              r,
          };
        }
      }
      return; // nothing judgeable, or all targets sanctioned
    } catch {
      return; // fail open — never brick edit/write on a guard bug
    }
  });
}
