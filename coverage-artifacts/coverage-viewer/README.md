
```
go run ./coverage-artifacts/coverage-viewer \
  -repo-root /home/katie.may@canonical.com/source/snapd \
  -results-dir /some/other/location/coverage-results \
  -functions-json \
  -test 'garden:ubuntu-24.04-64:tests--main--ack'
```

```
go run ./coverage-artifacts/coverage-viewer -functions-json -test 'garden:ubuntu-24.04-64:tests--main--ack' | jq '.files |= map(select(any(.covered_functions[]; (. != "init") and (endswith(".Name") | not))))'
```

```
go run ./coverage-artifacts/coverage-viewer -functions-json -test 'garden:ubuntu-24.04-64:tests--main--ack' \
  | jq -r '
      .files[]
      | select(
          any(.covered_functions[]; (. != "init") and (endswith(".Name") | not))
        )
      | .path
    '
```