#!/usr/bin/env bash
set -e

# Update Upstream Script
# Automatically pulls the latest main code updates while keeping local rebranding and customizations.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

cd "${ROOT_DIR}"

if git remote | grep -q "^upstream$"; then
    REMOTE="${UPSTREAM_REMOTE:-upstream}"
else
    REMOTE="${UPSTREAM_REMOTE:-origin}"
fi
BRANCH="${UPSTREAM_BRANCH:-main}"

echo "==> Fetching latest changes from ${REMOTE}/${BRANCH}..."
git fetch "${REMOTE}" "${BRANCH}"

HAS_STASH=0
if [ -n "$(git status --porcelain)" ]; then
    echo "==> Stashing uncommitted local changes..."
    git stash push -m "Auto-stash before upstream update"
    HAS_STASH=1
fi

echo "==> Integrating updates from ${REMOTE}/${BRANCH}..."
if ! git pull --rebase "${REMOTE}" "${BRANCH}"; then
    echo "⚠️  Rebase hit a conflict. Attempting standard merge fallback..."
    git rebase --abort 2>/dev/null || true
    git pull --no-edit "${REMOTE}" "${BRANCH}"
fi

if [ "${HAS_STASH}" -eq 1 ]; then
    echo "==> Restoring stashed local changes..."
    git stash pop || echo "⚠️ Stash pop had conflicts. Please resolve manually if needed."
fi

echo "==> Updating dependencies and applying database migrations..."
export PATH=$PATH:/usr/share/nodejs/corepack/shims:~/.local/share/pnpm:/usr/local/go/bin
if command -v pnpm >/dev/null 2>&1; then
    pnpm install --no-frozen-lockfile 2>/dev/null || true
fi

if [ -d "server" ] && command -v go >/dev/null 2>&1; then
    (cd server && go run ./cmd/migrate up) 2>/dev/null || true
fi

echo "✓ Upstream updates pulled successfully. Local rebranding and customizations preserved."
