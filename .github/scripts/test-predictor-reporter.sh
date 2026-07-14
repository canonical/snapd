#!/usr/bin/env bash

set -euo pipefail

repository="$1"
workflow_run_id="$2"
workflow_run_attempt="$3"
parser=".github/scripts/parse-results-predictor.py"
test_predictor_url="${TEST_PREDICTOR_URL:?TEST_PREDICTOR_URL must be set}"
test_predictor_url="${test_predictor_url%/}"

append_failure_section() {
	local verb="$1"
	local heading="$2"
	local failures=()

	mapfile -t failures < <(python3 "${parser}" failures consolidated-report.json "${verb}")
	if ((${#failures[@]} == 0)); then
		return
	fi

	echo "### ${heading}:"
	printf -- '- %s\n' "${failures[@]}"
}

# generate report
(
	date

	# The 'skip spread' label was added to the pull request
	if gh api /repos/"${repository}"/issues/"$(cat pr_number)" --jq '.labels.[].name' | grep -iq '^skip spread$'; then
		echo "## Spread tests skipped"
		exit 0
	fi
	echo "The following results are from: https://github.com/${repository}/actions/runs/${workflow_run_id}"

	# There are no logged spread failures
	if ! ls spread-results-"${workflow_run_id}"-*/*.json &>/dev/null; then
		echo '## No spread failures reported'

	else
		python3 "${parser}" consolidate consolidated-report.json spread-results-"${workflow_run_id}"*/*.json

		echo "## Failures:"
		append_failure_section "preparing" "Preparing"
		append_failure_section "executing" "Executing"
		append_failure_section "restoring" "Restoring"
	fi
) >report

if find . -name skipped_tests_list.txt | grep -q .; then
	{
		echo "## Skipped tests from [snapd-testing-skip](https://github.com/canonical/snapd-testing-skip)"
		echo "*If you wish to have any of the below tests run in your PR, in your PR description, add 'unskip:' followed by a copy-and-pasted list of the below tests you wish to run (unskip plus test list must be valid yaml)*"
		find . -name skipped_tests_list.txt -exec cat {} \; | tr ' ' '\n' | grep . | sed 's/:[^/:]*$//' | sort -u | awk '{print "- "$1}'
	} >>report
fi

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
		while IFS=$'\t' read -r display_name occurrences full_name system scenario; do
			if ((occurrences > 1)); then
				display_name+=" <kbd>${occurrences} times</kbd>"
			fi

			response=$(curl -sf -G "${test_predictor_url}/predict" \
				--max-time 10 \
				--data-urlencode "name=${full_name}" \
				--data-urlencode "verb=${verb}" \
				--data-urlencode "system=${system}" \
				--data-urlencode "scenario=${scenario}" \
				--data-urlencode "attempt=${workflow_run_attempt}" \
				2>/dev/null) || response='{}'
			probability=$(python3 "${parser}" success-probability <<<"$response")
			if [ -z "$probability" ]; then
				probability="unavailable"
			else
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

if [ -f consolidated-report.json ] && python3 "${parser}" has-predictor-rows consolidated-report.json; then
	echo "## Test Predictor Analysis" >>report
	append_predictor_table "preparing" "Preparing"
	append_predictor_table "executing" "Executing"
	append_predictor_table "restoring" "Restoring"
fi

# display the report
grep -n '' report
