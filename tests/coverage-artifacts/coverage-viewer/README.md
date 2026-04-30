
```bash
go run ./tests/coverage-artifacts/coverage-viewer \
  -repo-root /home/katie.may@canonical.com/source/snapd \
  -results-dir /some/other/location/coverage-results \
  -functions-json \
  -test 'garden:ubuntu-24.04-64:tests--main--ack'
```

```bash
go run ./tests/coverage-artifacts/coverage-viewer -functions-json -test 'garden:ubuntu-24.04-64:tests--main--ack' | jq '.files |= map(select(any(.covered_functions[]; (. != "init") and (endswith(".Name") | not))))'
```

```bash
go run ./tests/coverage-artifacts/coverage-viewer -functions-json -test 'garden:ubuntu-24.04-64:tests--main--ack' \
  | jq -r '
      .files[]
      | select(
          any(.covered_functions[]; (. != "init") and (endswith(".Name") | not))
        )
      | .path
    '
```

Find all files 
```bash
mkdir -p ./tests/coverage-artifacts/files
for dir in $(ls ./coverage-artifacts/coverage-results); do
  go run ./tests/coverage-artifacts/coverage-viewer -functions-json -test "$dir" \
    | jq -r '
      .files[]
      | select(
          any(.covered_functions[]; (. != "init") and (endswith(".Name") | not))
        )
      | .path
    ' > "coverage-artifacts/files/$dir"
done
```

Find all containing dirs for those files
```bash
mkdir -p ./tests/coverage-artifacts/dirs
for file in $(find ./tests/coverage-artifacts/files -type f); do
  sed 's#/[^/]*$##' "$file" | sort -u > "coverage-artifacts/dirs/$(basename "$file")"
done
```

Build a CSV with rows as files and columns as directories
```bash
mapfile -t dirs < <(
  find ./tests/coverage-artifacts/dirs -type f -print0 \
    | xargs -0 cat \
    | sort -u
)

{
  printf 'file'
  for dir in "${dirs[@]}"; do
    printf ',%s' "$dir"
  done
  printf '\n'

  while IFS= read -r -d '' file; do
    printf '%s' "${file##*/}"
    for dir in "${dirs[@]}"; do
      if grep -Fxq "$dir" "$file"; then
        printf ',true'
      else
        printf ',false'
      fi
    done
    printf '\n'
  done < <(find ./tests/coverage-artifacts/dirs -type f -print0 | sort -z)
} > coverage-artifacts/dirs-by-file.csv
```

For each test, list directories that are not common, excluding tests that do not test snapd code.
```bash
file_count=$(find ./tests/coverage-artifacts/dirs -type f -exec wc -l {} \; | grep -v '^0 ' | wc -l)

common=$(find ./tests/coverage-artifacts/dirs -type f -print0 \
  | xargs -0 cat \
  | sort \
  | uniq -c \
  | awk -v file_count="$file_count" '$1 == file_count { print $2 }')

mkdir -p ./tests/coverage-artifacts/unique-dirs
find ./tests/coverage-artifacts/dirs -type f -print0 \
  | while IFS= read -r -d '' file; do
      # Print only directories present in this file and not in the common set.
      missing=$(grep -Fvxf <(printf '%s\n' "$common") "$file")
      if [ -n "$missing" ]; then
        echo "$missing" > "./tests/coverage-artifacts/unique-dirs/$(basename "$file")"
      fi
    done

find ./tests/coverage-artifacts/dirs -type f -print0 \
  | xargs -0 cat \
  | sort \
  | uniq -c \
  | sort -rn
```

File coverage occurrences 
```bash
find ./tests/coverage-artifacts/files -type f -print0 \
  | xargs -0 cat \
  | sort \
  | uniq -c \
  | sort -rn
```


