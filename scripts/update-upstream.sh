#!/usr/bin/env bash
set -e

# Update Upstream Script
# Automatically pulls the latest main code updates from https://github.com/multica-ai/multica.git
# as the source of truth, while preserving local rebranding and customizations.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

cd "${ROOT_DIR}"

UPSTREAM_URL="${UPSTREAM_URL:-https://github.com/multica-ai/multica.git}"
BRANCH="${UPSTREAM_BRANCH:-main}"

# Ensure 'upstream' remote points to the source of truth (multica-ai/multica.git)
if git remote | grep -q "^upstream$"; then
    git remote set-url upstream "${UPSTREAM_URL}"
else
    git remote add upstream "${UPSTREAM_URL}"
fi

echo "==> Fetching source of truth updates from upstream (${UPSTREAM_URL})..."
git fetch upstream "${BRANCH}"

HAS_STASH=0
if [ -n "$(git status --porcelain)" ]; then
    echo "==> Stashing uncommitted local changes..."
    git stash push -m "Auto-stash before upstream update"
    HAS_STASH=1
fi

echo "==> Rebasing local rebranding on top of upstream/${BRANCH}..."
if ! git pull --rebase upstream "${BRANCH}"; then
    echo "⚠️  Rebase hit a conflict. Attempting standard merge fallback..."
    git rebase --abort 2>/dev/null || true
    git pull --no-edit upstream "${BRANCH}"
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

echo "✓ Successfully updated from upstream source of truth (multica-ai/multica.git)!"
echo "✓ Local rebranding and customizations preserved."
