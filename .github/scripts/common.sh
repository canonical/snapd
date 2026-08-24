#!/usr/bin/env bash

AUTO_RERUN_LABEL="Auto rerun spread"
SKIP_SPREAD_LABEL="Skip spread"
RUN_ONLY_ONE_SYSTEM_LABEL="Run only one system"
export NOT_RERUN_REASON=""

: "${GH_API_RETRIES:=3}"
: "${GH_API_RETRY_DELAY_SECONDS:=2}"
: "${GH_RETRY_CONTEXT:=}"

gh_retry() {
    local attempt=1
    local delay="$GH_API_RETRY_DELAY_SECONDS"
    local output
    local context="${GH_RETRY_CONTEXT:-}"

    while (( attempt <= GH_API_RETRIES )); do
        if output="$(gh "$@")"; then
            printf '%s\n' "$output"
            return 0
        fi

        if (( attempt == GH_API_RETRIES )); then
            if [[ -n "$context" ]]; then
                echo "Command failed after $GH_API_RETRIES attempts for $context: $*" >&2
            else
                echo "Command failed after $GH_API_RETRIES attempts: $*" >&2
            fi
            echo "$output" >&2
            return 1
        fi

        if [[ -n "$context" ]]; then
            echo "Transient failure on attempt $attempt/$GH_API_RETRIES for $context: $*" >&2
        else
            echo "Transient failure on attempt $attempt/$GH_API_RETRIES for: $*" >&2
        fi
        echo "$output" >&2

        sleep "$delay"
        delay=$((delay * 2))
        attempt=$((attempt + 1))
    done
}

pr_has_label() {
    local pr_json="$1"
    local label="$2"

    jq -e --arg label "$label" '[.labels[]?.name] | index($label) != null' <<<"$pr_json" >/dev/null
}

ensure_auto_rerun_label() {
    local pr_number="$1"
    local pr_json="$2"

    if pr_has_label "$pr_json" "$AUTO_RERUN_LABEL"; then
        return 0
    fi

    echo "Adding $AUTO_RERUN_LABEL label to PR #$pr_number"
    if ! gh_retry pr edit "$pr_number" --add-label "$AUTO_RERUN_LABEL"; then
        NOT_RERUN_REASON="failed to add label '$AUTO_RERUN_LABEL' to PR #$pr_number"
        return 1
    fi
}

# Returns a JSON object mapping each reviewer's login to their effective review
# state, determined by scanning the review history from newest to oldest and
# skipping COMMENTED reviews.  The first non-comment review found (i.e. the
# most recent actionable one) is the effective state:
#   APPROVED           -> "APPROVED"
#   CHANGES_REQUESTED  -> "CHANGES_REQUESTED"
#   DISMISSED          -> "CHANGES_REQUESTED"
pr_reviewer_effective_states() {
    local pr_json="$1"
    jq -r '
            [
                ((.reviews // .latestReviews // [])[]? | select(type == "object"))
                | {
                    login: (.author.login // ""),
                    submittedAt: (.submittedAt // ""),
                    state: (.state // "")
                  }
            ]
            | sort_by(.submittedAt)
            | reverse
            | reduce .[] as $r ({};
                if $r.login == "" or has($r.login) or $r.state == "COMMENTED" then
                    .
                else
                    .[$r.login] = (if $r.state == "APPROVED" then "APPROVED" else "CHANGES_REQUESTED" end)
                end
            )
    ' <<<"$pr_json" 2>/dev/null || echo '{}'
}

pr_with_reviews_history() {
    local pr_json="$1"
    local pr_number
    local reviews_json

    if [ "$(jq -r '(.reviews | type) == "array"' <<<"$pr_json" 2>/dev/null || echo false)" = "true" ]; then
        echo "$pr_json"
        return
    fi

    pr_number=$(jq -r '.number // empty' <<<"$pr_json")
    if [ -z "$pr_number" ]; then
        echo "$pr_json"
        return
    fi

    GH_RETRY_CONTEXT="PR #$pr_number review history lookup"
    if reviews_json=$(gh_retry pr view "$pr_number" --json reviews --jq '.reviews'); then
        pr_json=$(jq -c --argjson reviews "$reviews_json" '. + {reviews: $reviews}' <<<"$pr_json")
    fi
    GH_RETRY_CONTEXT=""

    echo "$pr_json"
}

pr_is_rerun_eligible() {
    local pr_json="$1"
    local min_approvals="$2"
    local require_auto_rerun_label="$3"
    local effective_states
    local changes_requested_count
    local approved_count

    if [ "$(jq -r '.isDraft' <<<"$pr_json")" = "true" ]; then
        NOT_RERUN_REASON="PR is a draft"
        return 1
    fi

    if pr_has_label "$pr_json" "$SKIP_SPREAD_LABEL" || pr_has_label "$pr_json" "$RUN_ONLY_ONE_SYSTEM_LABEL"; then
        NOT_RERUN_REASON="PR has blocking labels ($SKIP_SPREAD_LABEL or $RUN_ONLY_ONE_SYSTEM_LABEL)"
        return 1
    fi

    if [ "$require_auto_rerun_label" = "true" ] && ! pr_has_label "$pr_json" "$AUTO_RERUN_LABEL"; then
        NOT_RERUN_REASON="PR is missing the $AUTO_RERUN_LABEL label"
        return 1
    fi

    # gh pr list may omit full review history in some environments; fetch it on-demand.
    pr_json=$(pr_with_reviews_history "$pr_json")

    # Check if the PR has any reviews that would block a rerun
    effective_states=$(pr_reviewer_effective_states "$pr_json")
    if [ -z "$effective_states" ]; then
        effective_states='{}'
    fi
    if ! jq -e . >/dev/null 2>&1 <<<"$effective_states"; then
        effective_states='{}'
    fi
    echo "Effective reviewer states: $effective_states"

    changes_requested_count=$(jq -r '[.[] | select(. == "CHANGES_REQUESTED")] | length' <<<"$effective_states")
    approved_count=$(jq -r '[.[] | select(. == "APPROVED")] | length' <<<"$effective_states")

    if [ "$changes_requested_count" -gt 0 ]; then
        NOT_RERUN_REASON="PR has requested changes"
        return 1
    fi

    if [ "$approved_count" -lt "$min_approvals" ]; then
        NOT_RERUN_REASON="PR has fewer than $min_approvals approvals"
        return 1
    fi

    return 0
}

run_is_completed() {
    local run_json="$1"
    local run_id="$2"
    local run_status
    local run_conclusion

    run_status=$(jq -r '.status // empty' <<<"$run_json")
    run_conclusion=$(jq -r '.conclusion // empty' <<<"$run_json")

    if [ "$run_status" != "completed" ]; then
        NOT_RERUN_REASON="latest run_id=$run_id status=$run_status"
        return 1
    fi

    if [ "$run_conclusion" = "success" ]; then
        NOT_RERUN_REASON="latest run_id=$run_id completed successfully"
        return 1
    fi

    return 0
}

required_spread_failure_threshold_allows_rerun() {
    local run_id="$1"
    local pr_base="$2"
    local repo="$3"
    local max_failed_tasks="$4"
    local encoded_base
    local required_spread_checks
    local failed_required_system_targets=""
    local num_failed

    # Encode the branch name for use in the API URL
    encoded_base=$(jq -Rr @uri <<< "$pr_base")

    if ! required_spread_checks=$(gh_retry api \
        -X GET \
        -H "Accept: application/vnd.github+json" \
        "repos/$repo/rules/branches/$encoded_base" \
        --jq '[.[] | select(.type == "required_status_checks") | .parameters.required_status_checks[]?.context] | map(select(startswith("spread "))) | unique | .[]'); then
        NOT_RERUN_REASON="could not fetch branch protection rules for $pr_base"
        return 1
    fi

    if [ -z "$required_spread_checks" ]; then
        echo "No required checks detected for branch $pr_base; skipping required spread target filtering"
        return 0
    fi

    while IFS=$'\t' read -r failed_id failed_name; do
        if grep -Fxq "$failed_name" <<<"$required_spread_checks"; then
            failed_required_system_targets+="$failed_id "$'\n'
        fi
    done < <(gh_retry run view "$run_id" --json jobs --jq '.jobs[] | select(.name | test("^spread ")) | select(.conclusion == "failure") | [.databaseId, .name] | @tsv')

    for failed in $failed_required_system_targets; do
        num_failed=$(gh_retry run view --log-failed --job "$failed" | grep -oP '(?:\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}) Failed tasks: \K\d+$' | head -1 || true)

        if [ -n "$num_failed" ] && [ "$num_failed" -ge "$max_failed_tasks" ]; then
            NOT_RERUN_REASON="there were $max_failed_tasks or more failures on a required system target"
            return 1
        fi
    done

    return 0
}

predictor_allows_rerun() {
    local workflow_run_id="$1"
    local workflow_run_attempt="$2"
    local parser="$GITHUB_WORKSPACE/.github/scripts/parse-results-predictor.py"
    local test_predictor_url="${TEST_PREDICTOR_URL:-}"
    local threshold="${TEST_PREDICTOR_THRESHOLD:-0.5}"
    local results_dir artifact_rows artifact_id artifact_name
    local verb rows response probability
    local valid_predictions=0

    test_predictor_url="${test_predictor_url%/}"
    results_dir=$(mktemp -d)

    GH_RETRY_CONTEXT="list artifacts for run_id=$workflow_run_id"
    if ! artifact_rows=$(gh_retry api --paginate \
        "repos/$GH_REPO/actions/runs/$workflow_run_id/artifacts?per_page=100" \
        --jq ".artifacts[] | select(.name | startswith(\"spread-results-${workflow_run_id}-${workflow_run_attempt}\")) | [.id, .name] | @tsv"); then
        echo "Could not list artifacts for run_id=$workflow_run_id; allowing rerun"
        GH_RETRY_CONTEXT=""
        rm -rf "$results_dir"
        return 0
    fi
    GH_RETRY_CONTEXT=""

    if [[ -z "$artifact_rows" ]]; then
        echo "No spread result artifacts found for run_id=$workflow_run_id attempt=$workflow_run_attempt; allowing rerun"
        rm -rf "$results_dir"
        return 0
    fi

    while IFS=$'\t' read -r artifact_id artifact_name; do
        echo "Downloading artifact: $artifact_name.zip"
        if ! gh api \
            -H "Accept: application/vnd.github+json" \
            "repos/$GH_REPO/actions/artifacts/$artifact_id/zip" \
            >"$results_dir/$artifact_name.zip"; then
            echo "Could not download artifact $artifact_name; allowing rerun"
            rm -rf "$results_dir"
            return 0
        fi

        mkdir "$results_dir/$artifact_name"
        if ! unzip -q "$results_dir/$artifact_name.zip" -d "$results_dir/$artifact_name"; then
            echo "Could not unzip artifact $artifact_name; allowing rerun"
            rm -rf "$results_dir"
            return 0
        fi
    done <<<"$artifact_rows"

    if ! compgen -G "$results_dir/spread-results-${workflow_run_id}-*/*.json" >/dev/null; then
        echo "No spread result JSON files found for run_id=$workflow_run_id; allowing rerun"
        rm -rf "$results_dir"
        return 0
    fi

    if ! python3 "$parser" consolidate \
        "$results_dir/consolidated-report.json" \
        "$results_dir"/spread-results-"$workflow_run_id"-*/*.json; then
        echo "Could not consolidate spread results for run_id=$workflow_run_id; allowing rerun"
        rm -rf "$results_dir"
        return 0
    fi

    for verb in preparing executing restoring; do
        if ! rows=$(python3 "$parser" predictor-rows "$results_dir/consolidated-report.json" "$verb"); then
            echo "Could not parse $verb predictor rows; allowing rerun"
            rm -rf "$results_dir"
            return 0
        fi
        [[ -z "$rows" ]] && continue

        while IFS=$'\t' read -r display_name occurrences full_name system scenario; do
            if [[ -z "$test_predictor_url" ]]; then
                echo "Test predictor URL is unavailable; allowing rerun"
                rm -rf "$results_dir"
                return 0
            fi

            if ! response=$(curl -sf -G "$test_predictor_url/predict" \
                --connect-timeout 2 \
                --max-time 5 \
                --data-urlencode "name=$full_name" \
                --data-urlencode "verb=$verb" \
                --data-urlencode "system=$system" \
                --data-urlencode "scenario=$scenario" \
                --data-urlencode "attempt=$workflow_run_attempt" \
                2>/dev/null); then
                echo "Prediction unavailable for $display_name; allowing rerun"
                rm -rf "$results_dir"
                return 0
            fi

            if ! probability=$(python3 "$parser" success-probability <<<"$response") || [[ -z "$probability" ]]; then
                echo "Prediction malformed or invalid for $display_name; allowing rerun"
                rm -rf "$results_dir"
                return 0
            fi

            valid_predictions=$((valid_predictions + 1))
            if awk -v probability="$probability" -v threshold="$threshold" \
                'BEGIN { exit !(probability > threshold) }'; then
                echo "$display_name has predicted success probability $probability, above $threshold"
                rm -rf "$results_dir"
                return 0
            fi
        done <<<"$rows"
    done

    rm -rf "$results_dir"
    if ((valid_predictions == 0)); then
        echo "No test predictions were available; allowing rerun"
        return 0
    fi

    NOT_RERUN_REASON="all predicted success probabilities are at or below $threshold"
    return 1
}
