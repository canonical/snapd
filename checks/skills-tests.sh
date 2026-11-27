#!/bin/bash

set -eu

for scripts in .agents/skills/*/scripts; do
    # unittest exits 5 when no tests are found (python >= 3.12)
    if compgen -G "$scripts/test_*.py" >/dev/null; then
        PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s "$scripts" -p 'test_*.py'
    fi
done
