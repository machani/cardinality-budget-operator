#!/usr/bin/env bash
# Copyright 2026 Machani Vidyasagar.
# SPDX-License-Identifier: Apache-2.0
#
# Writes docs_only=true to $GITHUB_OUTPUT when a pull request changes only documentation (*.md files and
# docs/). Workflows then skip their remaining steps, but the job still reports success, so the required
# checks pass without a full run. Pushes to dev and main always run everything.
# Needs a checkout with fetch-depth 2: on a pull request HEAD is the merge commit and HEAD^1 the base branch.
set -euo pipefail

docs_only=false
if [ "${GITHUB_EVENT_NAME:-}" = "pull_request" ]; then
  changed=$(git diff --name-only HEAD^1 HEAD)
  if [ -n "$changed" ] && ! grep -qvE '(^docs/|\.md$)' <<<"$changed"; then
    docs_only=true
  fi
fi

echo "docs_only=$docs_only" >>"$GITHUB_OUTPUT"
echo "Docs-only change: $docs_only"
