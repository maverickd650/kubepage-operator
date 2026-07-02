#!/usr/bin/env bash
# Bootstraps the mise toolchain on Claude Code's remote/web sandbox, which
# starts with no mise installed — without this, an agent session can't run
# `mise run lint`/`mise run test`, the only supported entry points (see
# CLAUDE.md). No-op on local CLI sessions, which already have mise via the
# user's own environment.
set -euo pipefail

if [ "${CLAUDE_CODE_REMOTE:-}" != "true" ]; then
  exit 0
fi

if ! command -v mise >/dev/null 2>&1; then
  curl -fsSL https://mise.run | sh
  export PATH="$HOME/.local/bin:$PATH"
fi

mise trust
mise install

# Both dirs go on PATH for the rest of the session: the shims dir for the
# pinned tools, and ~/.local/bin for the mise binary itself (where the
# installer above puts it — shims invoke mise, so they're dead without it).
shims_dir="$HOME/.local/share/mise/shims"
export PATH="$shims_dir:$HOME/.local/bin:$PATH"

if [ -n "${CLAUDE_ENV_FILE:-}" ]; then
  echo "export PATH=\"$shims_dir:$HOME/.local/bin:\$PATH\"" >> "$CLAUDE_ENV_FILE"
fi

# Pre-fetch what `mise run test` needs so the first real invocation doesn't
# pay the download cost mid-session; best-effort so a flaky download here
# never blocks session startup (exit 1 from a SessionStart hook is a
# blocking error).
go mod download || true

if k8s_version="$(go list -m k8s.io/api 2>/dev/null | sed -E 's/.*v[0-9]+\.([0-9]+).*/1.\1/')" && [ -n "$k8s_version" ]; then
  mkdir -p bin
  setup-envtest use "$k8s_version" --bin-dir "$PWD/bin" -p path >/dev/null || true
fi
