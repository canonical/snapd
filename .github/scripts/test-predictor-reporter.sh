#!/usr/bin/env bash

set -euo pipefail

repository="$1"
workflow_run_id="$2"
workflow_run_attempt="$3"
parser=".github/scripts/parse-results-predictor.py"
test_predictor_url="${TEST_PREDICTOR_URL:-}"
test_predictor_url="${test_predictor_url%/}"

# generate report
(
	date

	# The 'skip spread' label was added to the pull request
	if gh api /repos/"${repository}"/issues/"$(cat pr_number)" --jq '.labels.[].name' | grep -iq '^skip spread$'; then
		echo "## Spread tests skipped"
		exit 0
	fi
	echo "The following results are from: https://github.com/${repository}/actions/runs/${workflow_run_id}"

	# There are no downloaded spread results artifacts.
	if ! ls spread-results-"${workflow_run_id}"-*/*.json &>/dev/null; then
		echo '## No spread result artifacts found'
		echo 'No spread results JSON artifacts were available for this workflow run, so spread failures (if any) could not be reported.'

	else
		python3 "${parser}" consolidate consolidated-report.json spread-results-"${workflow_run_id}"*/*.json
	fi
) >report

append_predictor_table() {
	local verb="$1"
	local heading="$2"
	local predictor_rows=()

	mapfile -t predictor_rows < <(python3 "${parser}" predictor-rows consolidated-report.json "${verb}")
	if ((${#predictor_rows[@]} == 0)); then
		return
	fi

	{
		echo "### ${heading}"
		echo "| Test | Success % |"
		echo "|------|-----------|"
	} >>report

	printf '%s\n' "${predictor_rows[@]}" |
		while IFS=$'\t' read -r display_name occurrences full_name system scenario source_group; do
			if ((occurrences > 1)); then
				display_name+=" <kbd>${occurrences} times</kbd>"
			fi
			if [ -n "$source_group" ]; then
				printf '%s\n' "$source_group" >>predictor-groups-seen
			fi

			response='{}'
			if [ -n "${test_predictor_url}" ]; then
				response=$(curl -sf -G "${test_predictor_url}/predict" \
					--max-time 10 \
					--data-urlencode "name=${full_name}" \
					--data-urlencode "verb=${verb}" \
					--data-urlencode "system=${system}" \
					--data-urlencode "scenario=${scenario}" \
					--data-urlencode "attempt=${workflow_run_attempt}" \
					2>/dev/null) || response='{}'
			fi
			probability=$(python3 "${parser}" success-probability <<<"$response")
			if [ -z "$probability" ]; then
				if [ -n "$source_group" ]; then
					printf '%s\n' "$source_group" >>predictor-groups-ineligible
				fi
				probability="unavailable"
			else
				if [ -n "$source_group" ] && ! awk -v probability="$probability" 'BEGIN { exit !(probability > 0.5) }'; then
					printf '%s\n' "$source_group" >>predictor-groups-ineligible
				fi
				probability=$(awk -v probability="$probability" 'BEGIN {
                    if (probability >= 0.8) marker = "🟢";
                    else if (probability >= 0.3) marker = "🟡";
                    else marker = "🔴";
                    printf "%s %.1f%%", marker, probability * 100
                  }')
			fi

			echo "| ${display_name} | ${probability} |" >>report
		done

	echo "" >>report
}

rerun_predictor_jobs() {
	if [ "$workflow_run_attempt" != 1 ]; then
		return
	fi

	comm -23 \
		<(sort -u predictor-groups-seen) \
		<(sort -u predictor-groups-ineligible) >predictor-rerun-groups
	if [ ! -s predictor-rerun-groups ]; then
		return
	fi

	if ! gh api --paginate "/repos/${repository}/actions/runs/${workflow_run_id}/jobs?filter=latest&per_page=100" \
		--jq '.jobs[] | [.id, .name] | @tsv' >workflow-jobs; then
		echo "Unable to retrieve workflow jobs; predictor-selected tests were not retried." >>report
		return
	fi

	echo "### Predictor-selected retries" >>report
	while IFS= read -r group; do
		job_id=""
		while IFS=$'\t' read -r candidate_id job_name; do
			if [[ "$job_name" == "spread ${group} /"* ]]; then
				job_id="$candidate_id"
				break
			fi
		done <workflow-jobs

		if [ -z "$job_id" ]; then
			echo "- Could not find the completed spread job for \`${group}\`." >>report
		elif gh api --method POST "/repos/${repository}/actions/jobs/${job_id}/rerun" >/dev/null; then
			echo "- Retrying failed tests from \`${group}\` (all predictor scores > 50%)." >>report
		else
			echo "- Failed to request a retry for \`${group}\`." >>report
		fi
	done <predictor-rerun-groups
	echo "" >>report
}

if [ -f consolidated-report.json ] && python3 "${parser}" has-predictor-rows consolidated-report.json; then
	: >predictor-groups-seen
	: >predictor-groups-ineligible
	echo "## Test Predictor Analysis" >>report
	append_predictor_table "preparing" "Preparing"
	append_predictor_table "executing" "Executing"
	append_predictor_table "restoring" "Restoring"
	rerun_predictor_jobs
fi

if find . -name skipped_tests_list.txt | grep -q .; then
	{
		echo "## Skipped tests from [snapd-testing-skip](https://github.com/canonical/snapd-testing-skip)"
		echo "*If you wish to have any of the below tests run in your PR, in your PR description, add 'unskip:' followed by a copy-and-pasted list of the below tests you wish to run (unskip plus test list must be valid yaml)*"
		find . -name skipped_tests_list.txt -exec cat {} \; | tr ' ' '\n' | grep . | sed 's/:[^/:]*$//' | sort -u | awk '{print "- "$1}'
	} >>report
fi

# display the report
grep -n '' report
