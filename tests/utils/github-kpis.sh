#!/bin/bash

set -euo pipefail

show_help() {
    cat <<'EOF'
Usage: github-kpis.sh (--start YYYY-MM-DD [--end YYYY-MM-DD] | --input-json PATH) [--attempts] [--forced] [--skipped] [--runtime] [--test-totals] [--all]

If the script errors at any point, it will output the JSON collected before that stage.
To resume from that JSON, save it to a file and use --input-json with the path to that file. 
You can also use --input-json - to read from stdin.

Ex: 
To get all data for PRs merged starting from May 1st 2026 (included):
./github-kpis.sh --start 2026-05-01 --all > pr_data.json

To calculate only force merge data on a previously calculated JSON file:
./github-kpis.sh --input-json pr_data.json --forced > pr_data_with_forced.json
or
cat pr_data.json | ./github-kpis.sh --input-json - --forced > pr_data_with_forced.json

Options:
  --start DATE    Required. Start merged date (inclusive).
  --end DATE      Optional. End merged date (inclusive, day granularity).
  --input-json    Resume from an existing JSON file instead of fetching PRs. Use - for stdin.
  --attempts      Add number of attempts made of running the Tests workflow on the last PR update before merging.
  --forced        Add field to specify whether or not the PR was force merged.
  --skipped       Add field to specify how many tests (excluding variants) were skipped via snapd-testing-skip.
  --runtime       Add total runtime of all attempts of the Tests workflow on the last PR update before merging.
  --test-totals   Add total number of spread tests run on the last PR update before merging.
  --all           Add all of the above fields.
  -h, --help      Show this help.
EOF
}

gh_with_retry() {
    local output exit_code attempts=0
    while [ $attempts -lt 5 ]; do
        output="$(gh "$@" 2>&1)"
        exit_code=$?
        if [ $exit_code -eq 0 ]; then
            echo "$output"
            return 0
        fi
        if echo "$output" | grep -q 'HTTP 50'; then
            attempts=$(( attempts + 1 ))
            echo "received: $output (attempt $attempts/5), retrying in 1s..." >&2
            sleep 1
        else
            echo "$output" >&2
            return $exit_code
        fi
    done
    echo "gh command failed after 5 retries: gh $*" >&2
    echo "$output" >&2
    return $exit_code
}

progress_init() {
    local label="$1"
    local total="$2"
    PROGRESS_LABEL="$label"
    PROGRESS_TOTAL="$total"
    PROGRESS_DONE=0
    PROGRESS_WIDTH=30
}

progress_tick() {
    local filled empty
    if [ -z "${PROGRESS_TOTAL:-}" ] || (( PROGRESS_TOTAL <= 0 )); then
        return
    fi

    PROGRESS_DONE=$(( PROGRESS_DONE + 1 ))
    filled=$(( PROGRESS_DONE * PROGRESS_WIDTH / PROGRESS_TOTAL ))
    empty=$(( PROGRESS_WIDTH - filled ))

    printf '\r%s progress: [%s%s] %d/%d' \
        "$PROGRESS_LABEL" \
        "$(printf '%*s' "$filled" '' | tr ' ' '#')" \
        "$(printf '%*s' "$empty" '' | tr ' ' '-')" \
        "$PROGRESS_DONE" \
        "$PROGRESS_TOTAL" >&2

    if (( PROGRESS_DONE == PROGRESS_TOTAL )); then
        printf '\n' >&2
    fi
}

get_total_tests_run() {
    local prs_json="$1"
    local pr id attempts attempt tmpdir artifact_path json_file
    local total_prs

    total_prs="$(jq 'length' <<< "$prs_json")"
    progress_init "Test totals" "$total_prs"

    while IFS= read -r pr; do
        if [ "$(jq -r '.["spread-skipped"]' <<< "$pr")" = "true" ]; then
            jq -c '. + {"total-tests-run": 0}' <<< "$pr"
        else
            id="$(jq -r '.["databaseId"]' <<< "$pr")"
            attempts="$(jq -r '.["num-attempts"]' <<< "$pr")"
            
            if [ -z "$id" ] || [ "$id" = "null" ] || [ "$attempts" = "0" ]; then
                jq -c '. + {"total-tests-run": null}' <<< "$pr"
                progress_tick
                continue
            fi
            
            tmpdir=$(mktemp -d)
            trap 'if [ -n "${tmpdir:-}" ] && [ -d "$tmpdir" ]; then rm -rf "$tmpdir"; fi' RETURN
            gh_with_retry run download "$id" --repo canonical/snapd --dir "$tmpdir" --pattern "spread-results-*"
            
            local total_tests=0
            local dir_name system attempt_num
            local -A first_attempt_for_system=()
            local -A first_file_for_system=()

            while IFS= read -r -d '' json_file; do
                dir_name="$(basename "$(dirname "$json_file")")"
                if [[ "$dir_name" =~ ^spread-results-[0-9]+-([0-9]+)-(.*)$ ]]; then
                    attempt_num="${BASH_REMATCH[1]}"
                    system="${BASH_REMATCH[2]}"
                    if [[ -z "${first_attempt_for_system[$system]+x}" ]] || (( attempt_num < first_attempt_for_system[$system] )); then
                        first_attempt_for_system[$system]="$attempt_num"
                        first_file_for_system[$system]="$json_file"
                    fi
                fi
            done < <(find "$tmpdir" -type f -name "results.json" -print0)

            for system in "${!first_file_for_system[@]}"; do
                json_file="${first_file_for_system[$system]}"
                if [ -f "$json_file" ]; then
                    local count="$(jq '(.results["task-passed"]) + (.results["task-failed"]) + (.results["task-aborted"]) + (.results["task-restore-failed"])' "$json_file")"
                    total_tests=$(( total_tests + count ))
                fi
            done
            
            jq -c --argjson total "$total_tests" '. + {"total-tests-run": $total}' <<< "$pr"
            
            rm -rf "$tmpdir"
            tmpdir=""
            trap - RETURN
        fi
        progress_tick
    done < <(jq -c '.[]' <<< "$prs_json") | jq -s '.'
}

get_total_runtime() {
    local prs_json="$1"
    local pr id attempt attempts total_runtime runtime first_attempt_runtime first_attempt_only_fundamental json_fields
    local total_prs

    total_prs="$(jq 'length' <<< "$prs_json")"
    progress_init "Runtime" "$total_prs"

    while IFS= read -r pr; do
        if [ "$(jq -r '.["spread-skipped"]' <<< "$pr")" = "true" ]; then
            jq -c '. + {"total-runtime-minutes": null, "first-attempt-minutes": null, "first-attempt-only-fundamental": null}' <<< "$pr"
        else
            id="$(jq -r '.["databaseId"]' <<< "$pr")"
            attempts="$(jq -r '.["num-attempts"]' <<< "$pr")"
            if [ -z "$id" ] || [ "$id" = "null" ] || [ "$attempts" = "0" ]; then
                jq -c '. + {"total-runtime-minutes": null, "first-attempt-minutes": null, "first-attempt-only-fundamental": null}' <<< "$pr"
                progress_tick
                continue
            fi
            total_runtime=0
            first_attempt_runtime=null
            first_attempt_only_fundamental=false
            for attempt in $(seq 1 "$attempts"); do
                json_fields="startedAt,updatedAt"
                if [ "$attempt" = "1" ]; then
                    json_fields="$json_fields,jobs"
                fi
                object=$(gh_with_retry run view "$id" --repo canonical/snapd --attempt "$attempt" --json "$json_fields")
                runtime=$(jq '(((.updatedAt | fromdateiso8601) - (.startedAt | fromdateiso8601)) / 60 | floor)' <<<"$object")
                if ! [[ "$runtime" =~ ^[0-9]+$ ]]; then
                    echo "warning: failed to get runtime for PR #$(jq -r '.number' <<< "$pr") attempt $attempt, got: $runtime" >&2
                    return 1
                fi
                if [ "$attempt" = "1" ]; then
                    first_attempt_runtime=$runtime
                    # There are two non-fundamental systems jobs that are mutually exclusive. If both have not run, yet fundamental
                    # jobs have, then there will be exactly two 'spread ${{ matrix.group }}' jobs.
                    if jq -r '(.jobs // []).[].name' <<<"$object" | sort | uniq -c | grep -q '2 spread ${{ matrix.group }}'; then
                        first_attempt_only_fundamental=true
                    fi
                fi
                total_runtime=$(( total_runtime + runtime ))
            done
            jq -c --argjson total "$total_runtime" --argjson first "$first_attempt_runtime" --argjson fundamental "$first_attempt_only_fundamental" '. + {"total-runtime-minutes": $total, "first-attempt-minutes": $first, "first-attempt-only-fundamental": $fundamental}' <<< "$pr"
        fi
        progress_tick
    done < <(jq -c '.[]' <<< "$prs_json") | jq -s '.'
}

get_skipped_tests() {
    local prs_json="$1"
    local pr number num_skipped
    local total_prs

    total_prs="$(jq 'length' <<< "$prs_json")"
    progress_init "Skipped tests" "$total_prs"

    while IFS= read -r pr; do
        if [ "$(jq -r '.["spread-skipped"]' <<< "$pr")" = "true" ]; then
            jq -c '. + {"num-skipped": 0}' <<< "$pr"
        else
            number="$(jq -r '.number' <<< "$pr")"
            num_skipped="$(gh_with_retry pr view "$number" --repo canonical/snapd --json comments --jq '.comments.[] | select(.author.login == "github-actions") | .body' \
                                | sed -n '/## Skipped/,$ p' \
                                | grep '^- ' \
                                | sed 's/^- //' \
                                | awk -F':' '{print $1 ":" $2 ":" $3}' \
                                | sort -u \
                                | wc -l)"
            jq -c --argjson skipped "$num_skipped" '. + {"num-skipped": $skipped}' <<< "$pr"
        fi
        progress_tick
    done < <(jq -c '.[]' <<< "$prs_json") | jq -s '.'
}

get_force_merged() {
    local prs_json="$1"
    local pr number num_not_passed
    local total_prs

    total_prs="$(jq 'length' <<< "$prs_json")"
    progress_init "Force-merged" "$total_prs"

    while IFS= read -r pr; do
        if [ "$(jq -r '.["spread-skipped"]' <<< "$pr")" = "true" ]; then
            jq -c '. + {"force-merged": false}' <<< "$pr"
        else
            number="$(jq -r '.number' <<< "$pr")"
            num_not_passed="$(gh_with_retry pr checks "$number" --repo canonical/snapd --required --json bucket --jq '[.[].bucket | select(. != "pass")] | length' 2>/dev/null || echo 0)"
            if (( num_not_passed > 0 )); then
                jq -c '. + {"force-merged": true}' <<< "$pr"
            else
                jq -c '. + {"force-merged": false}' <<< "$pr"
            fi
        fi
        progress_tick
    done < <(jq -c '.[]' <<< "$prs_json") | jq -s '.'
}

get_num_attempts() {
    local prs_json="$1"
    local pr commit run
    local total_prs

    total_prs="$(jq 'length' <<< "$prs_json")"
    progress_init "Attempts" "$total_prs"

    while IFS= read -r pr; do
        if [ "$(jq -r '.["spread-skipped"]' <<< "$pr")" = "true" ]; then
            jq -c '. + {"num-attempts": 1, "databaseId": null}' <<< "$pr"
        else
            commit="$(jq -r '.headRefOid' <<< "$pr")"
            run="$(gh_with_retry run list --repo canonical/snapd --commit "$commit" --workflow 'ci-test.yaml' --json 'attempt,databaseId' --jq 'first(.[] | {attempt: (.attempt // 0), databaseId: .databaseId}) // {attempt: 0, databaseId: null}')"
            jq -c --argjson run "$run" '. + {"num-attempts": $run.attempt, "databaseId": $run.databaseId}' <<< "$pr"
        fi
        progress_tick
    done < <(jq -c '.[]' <<< "$prs_json") | jq -s '.'
}

prs() {
    local start_date="$1"
    local end_date="${2:-}"

    list_prs_in_range() {
        local start_epoch="$1"
        local end_epoch="$2"
        local start_iso end_iso json count mid_epoch

        start_iso="$(date -u -d "@$start_epoch" +"%Y-%m-%dT%H:%M:%SZ")"
        end_iso="$(date -u -d "@$end_epoch" +"%Y-%m-%dT%H:%M:%SZ")"

        json="$(gh_with_retry pr list --repo canonical/snapd --limit 1000 --search "merged:>=$start_iso merged:<$end_iso" --json number,mergedAt,headRefOid,labels)"
        count="$(jq 'length' <<< "$json")"

        if (( count < 1000 )); then
            jq -c '
                .[]
                | . + {
                    "spread-skipped": any((.labels // [])[].name; . == "Skip spread"),
                    nested: any((.labels // [])[].name; . == "Run nested")
                }
                | del(.labels)
            ' <<< "$json"
            return
        fi

        # If this still has 1000 in a tiny window of an hour, stop rather than silently truncating.
        if (( end_epoch - start_epoch <= 3600 )); then
            echo "cannot safely paginate: >1000 PRs in one hour between $start_iso and $end_iso" >&2
            return 1
        fi

        mid_epoch=$(( (start_epoch + end_epoch) / 2 ))
        list_prs_in_range "$start_epoch" "$mid_epoch"
        list_prs_in_range "$mid_epoch" "$end_epoch"
    }

    local start_epoch end_epoch
    start_epoch="$(date -u -d "$start_date" +%s)"

    if [ -n "$end_date" ]; then
        # Treat end_date as day-inclusive by converting to an exclusive upper bound.
        end_epoch="$(date -u -d "$end_date + 1 day" +%s)"
    else
        # No explicit end date: use tomorrow at current UTC time as an exclusive bound.
        end_epoch="$(date -u -d "+ 1 day" +%s)"
    fi

    list_prs_in_range "$start_epoch" "$end_epoch" | jq -s '.'
}

run_stage() {
    local stage_label="$1"
    local stage_func="$2"
    local pending_steps="$3"
    local current_json="$4"
    local next_json

    if next_json="$("$stage_func" "$current_json")"; then
        printf '%s' "$next_json"
        return 0
    fi

    echo "error: failed during step: $stage_label" >&2
    if [ -n "$pending_steps" ]; then
        echo "missing requested steps: $stage_label $pending_steps" >&2
    else
        echo "missing requested steps: none" >&2
    fi
    printf '%s\n' "$current_json"
    return 1
}

main() {
    local start_date=""
    local end_date=""
    local input_json_path=""
    local include_attempts=false
    local include_forced=false
    local include_skipped=false
    local include_runtime=false
    local include_test_totals=false
    local result=""
    local pending_steps=()

    if [ $# -eq 0 ]; then
        show_help
        exit 1
    fi

    while [ $# -gt 0 ]; do
        case "$1" in
            --start)
                if [ $# -lt 2 ]; then
                    echo "missing value for --start" >&2
                    exit 1
                fi
                start_date="$2"
                shift 2
                ;;
            --end)
                if [ $# -lt 2 ]; then
                    echo "missing value for --end" >&2
                    exit 1
                fi
                end_date="$2"
                shift 2
                ;;
            --input-json)
                if [ $# -lt 2 ]; then
                    echo "missing value for --input-json" >&2
                    exit 1
                fi
                input_json_path="$2"
                shift 2
                ;;
            --attempts)
                include_attempts=true
                shift
                ;;
            --forced)
                include_forced=true
                shift
                ;;
            --skipped)
                include_skipped=true
                shift
                ;;
            --runtime)
                include_runtime=true
                shift
                ;;
            --test-totals)
                include_test_totals=true
                shift
                ;;
            --all)
                include_attempts=true
                include_forced=true
                include_skipped=true
                include_runtime=true
                include_test_totals=true
                shift
                ;;
            -h|--help)
                show_help
                exit 0
                ;;
            *)
                echo "unknown argument: $1" >&2
                show_help
                exit 1
                ;;
        esac
    done

    if [ -n "$start_date" ] && [ -n "$input_json_path" ]; then
        echo "use either --start/--end or --input-json, not both" >&2
        show_help
        exit 1
    fi

    if [ -z "$start_date" ] && [ -z "$input_json_path" ]; then
        echo "either --start or --input-json is required" >&2
        show_help
        exit 1
    fi

    if [ -n "$input_json_path" ]; then
        if [ "$input_json_path" = "-" ]; then
            echo "Loading input JSON from stdin..." >&2
            result="$(jq -c '.')"
        else
            echo "Loading input JSON from $input_json_path..." >&2
            result="$(jq -c '.' "$input_json_path")"
        fi
        echo "Input PRs loaded: $(jq 'length' <<< "$result")" >&2
    else
        echo "Fetching PRs merged between $start_date and ${end_date:-now}..." >&2
        result="$(prs "$start_date" "$end_date")"
        echo "PRs fetched: $(jq 'length' <<< "$result")" >&2
    fi

    if [ "$include_attempts" = true ]; then
        pending_steps+=("attempts")
    fi

    if [ "$include_forced" = true ]; then
        pending_steps+=("forced")
    fi

    if [ "$include_skipped" = true ]; then
        pending_steps+=("skipped")
    fi

    if [ "$include_runtime" = true ]; then
        pending_steps+=("runtime")
    fi

    if [ "$include_test_totals" = true ]; then
        pending_steps+=("test-totals")
    fi

    if [ "$include_attempts" = true ]; then
        echo "Fetching number of attempts for each PR..." >&2
        pending_steps=("${pending_steps[@]:1}")
        if ! result="$(run_stage "attempts" get_num_attempts "${pending_steps[*]}" "$result")"; then
            echo "$result"
            exit 1
        fi
        echo "Done." >&2
    fi

    if [ "$include_forced" = true ]; then
        echo "Determining whether each PR was force merged..." >&2
        pending_steps=("${pending_steps[@]:1}")
        if ! result="$(run_stage "forced" get_force_merged "${pending_steps[*]}" "$result")"; then
            echo "$result"
            exit 1
        fi
        echo "Done." >&2
    fi

    if [ "$include_skipped" = true ]; then
        echo "Determining number of skipped tests for each PR..." >&2
        pending_steps=("${pending_steps[@]:1}")
        if ! result="$(run_stage "skipped" get_skipped_tests "${pending_steps[*]}" "$result")"; then
            echo "$result"
            exit 1
        fi
        echo "Done." >&2
    fi

    if [ "$include_runtime" = true ]; then
        echo "Calculating total runtime for each PR..." >&2
        pending_steps=("${pending_steps[@]:1}")
        if ! result="$(run_stage "runtime" get_total_runtime "${pending_steps[*]}" "$result")"; then
            echo "$result"
            exit 1
        fi
        echo "Done." >&2
    fi

    if [ "$include_test_totals" = true ]; then
        echo "Calculating test totals for each PR..." >&2
        pending_steps=("${pending_steps[@]:1}")
        if ! result="$(run_stage "test-totals" get_total_tests_run "${pending_steps[*]}" "$result")"; then
            echo "$result"
            exit 1
        fi
        echo "Done." >&2
    fi

    echo "$result"
}

main "$@"
