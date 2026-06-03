#!/bin/bash

tests_to_execute_from_file() {
    file_changed="$1"
    file_dir="$2"
    for file in $(find "$file_dir" -type f); do
        if grep -q "$file_changed" "$file"; then
            dir=$(basename "$file")
            echo "${dir//--/\/}"
        fi
    done
}

tests_to_execute_from_dir() {
    dir_changed="$1"
    dir_dir="$2"
    for file in $(find "$dir_dir" -type f); do
        if grep -q "$dir_changed" "$file"; then
            dir=$(basename "$file")
            echo "${dir//--/\/}"
        fi
    done
}

get_num_tests_to_execute_by_file() {
    file_dir="$1"
    go_source=$(find . -type f -name "*.go" | grep -v "vendor" | grep -v "tests" | grep -v "_test.go")
    for file in $go_source; do
        f="${file#./}"
        echo "$(tests_to_execute_from_file "$f" "$file_dir" | wc -l) $f"
    done
}

get_num_tests_to_execute_by_dir() {
    dir_dir="$1"
    go_source=$(find . -type f -name "*.go" | grep -v "vendor" | grep -v "tests" | grep -v "_test.go" | xargs -n1 dirname | sort -u)
    for dir in $go_source; do
        d="${dir#./}"
        echo "$(tests_to_execute_from_dir "$d" "$dir_dir" | wc -l) $d"
    done
}

calculate_all_files() {
    out_dir="$1"
    filter_init="${2:-true}"
    mkdir -p "$out_dir"
    for dir in $(ls ./tests/coverage-artifacts/coverage-results); do
        if [ "$filter_init" == "true" ]; then
            go run ./tests/coverage-artifacts/coverage-viewer -functions-json -test "$dir" \
                | jq -r '
                .files[]
                | select(any(.covered_functions[]; endswith(".Name") | not))
                | .path
                ' > "$out_dir/$dir"
        else
            go run ./tests/coverage-artifacts/coverage-viewer -functions-json -test "$dir" \
                | jq -r '
                .files[]
                | .path
                ' > "$out_dir/$dir"
        fi
    done
}

calculate_all_dirs() {
    out_dir="$1"
    file_dir="$2"
    mkdir -p "$out_dir"
    for file in $(find "$file_dir" -type f); do
        sed 's#/[^/]*$##' "$file" | sort -u > "$out_dir/$(basename "$file")"
    done
}

calculate_all_files_and_dirs() {
    out_dir="$1"
    filter_init="${2:-true}"
    calculate_all_files "$out_dir/files" "$filter_init"
    calculate_all_dirs "$out_dir/dirs" "$out_dir/files"
}