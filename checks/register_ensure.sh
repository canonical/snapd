#!/bin/bash

set -eu

missing=0
while IFS=' ' read -r manager func; do
    if ! grep -rq "swfeats.RegisterEnsure(\"$manager\",.*\"$func\")" .; then
        echo "Missing ensure function registration. Add the following to the relevant file: swfeats.RegEnsure(\"$manager\", \"$func\")"
        missing=1
    fi
done < <(grep --exclude '*_test.go'  --exclude 'ensurelogchecker.go' -r '.Trace("ensure"' overlord | sed -n 's/.*\.Trace("ensure",.*"manager",.*"\([^"]*\)",.*"func",.*"\([^"]*\)").*/\1 \2/p')

if [ $missing -eq 1 ]; then
    exit 1
fi
