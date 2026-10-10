#!/usr/bin/env bash
# Copyright 2026 Machani Vidyasagar.
# SPDX-License-Identifier: Apache-2.0
#
# Fails if any Go file in the repository lacks the Apache-2.0 license header.
# Checks tracked and new (untracked, not ignored) files. Run with: make verify-license
set -euo pipefail

header='Licensed under the Apache License, Version 2.0'

missing=$(git ls-files --cached --others --exclude-standard -- '*.go' | while read -r f; do
  grep -q "$header" "$f" || echo "$f"
done)

if [ -n "$missing" ]; then
  echo "These Go files are missing the Apache-2.0 header (copy it from hack/boilerplate.go.txt):"
  echo "$missing"
  exit 1
fi

echo "License headers OK"
