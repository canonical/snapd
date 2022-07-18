#! /bin/sh

CURRENT_YEAR="$(date +%Y)"
AUTHOR_TAG="${1:-"Canonical Ltd"}"

for i in $(git ls-files | grep '.\(h\|c\|go\)$')
do
    LINE="$(grep Copyright.*"${AUTHOR_TAG}" "$i")"
    SINGLE_YEAR=$(echo "$LINE" | sed -n 's/ \* Copyright .* \([0-9]\+\) .*/\1/p')
    RANGE_YEAR=$(echo "$LINE" | sed -n 's/ \* Copyright .* [0-9]\+-\([0-9]\+\) .*/\1/p')
    if [ -n "$SINGLE_YEAR" ] && [ "$SINGLE_YEAR" -ne "$CURRENT_YEAR" ]; then
        sed -i"" -e 's/\( \* Copyright .*\) '"$SINGLE_YEAR"' \(.*\)/\1 '"${SINGLE_YEAR}-$CURRENT_YEAR"' \2/' "$i"
    elif [ -n "$RANGE_YEAR" ]; then
        sed -i"" -e 's/\( \* Copyright .*\)-'"$RANGE_YEAR"' \(.*\)/\1-'"$CURRENT_YEAR"' \2/' "$i"
    fi
done
