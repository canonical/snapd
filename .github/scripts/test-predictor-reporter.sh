#!/usr/bin/env bash

set -euo pipefail

repository="$1"
workflow_run_id="$2"
workflow_run_attempt="$3"

# generate report
(
	date

	# The 'skip spread' label was added to the pull request
	if gh api /repos/"${repository}"/issues/"$(cat pr_number)" | jq '.labels.[].name' | grep -iq '"skip spread"'; then
		echo "## Spread tests skipped"
		exit 0
	fi
	echo "The following results are from: https://github.com/${repository}/actions/runs/${workflow_run_id}"

	# There are no logged spread failures
	if ! ls spread-results-"${workflow_run_id}"-*/*.json &>/dev/null; then
		echo '## No spread failures reported'

	else
		# Consolidate all json files into one
		jq -s '{items: (map(.items[]) )}' spread-results-"${workflow_run_id}"*/*.json >consolidated-report.json

		echo "## Failures:"
		if [[ $(jq -r '.items[] | select(.verb == "preparing" and .success == false)' consolidated-report.json) ]]; then
			echo "### Preparing:"
			jq -r '.items[] | select(.verb == "preparing" and .success == false) | "\(.backend):\(.system):\(.name)\(if .variant != "" then ":\(.variant)" else "" end)"' consolidated-report.json |
				awk ' { print "- " $0 }'
		fi
		if [[ $(jq -r '.items[] | select(.verb == "executing" and .success == false)' consolidated-report.json) ]]; then
			echo "### Executing:"
			jq -r '.items[] | select(.verb == "executing" and .success == false) | "\(.backend):\(.system):\(.name)\(if .variant != "" then ":\(.variant)" else "" end)"' consolidated-report.json |
				awk ' { print "- " $0 }'
		fi
		if [[ $(jq -r '.items[] | select(.verb == "restoring" and .success == false)' consolidated-report.json) ]]; then
			echo "### Restoring:"
			jq -r '.items[] | select(.verb == "restoring" and .success == false) | "\(.backend):\(.system):\(.name)\(if .variant != "" then ":\(.variant)" else "" end)"' consolidated-report.json |
				awk ' { print "- " $0 }'
		fi
	fi
) >report

if find . -name skipped_tests_list.txt | grep -q .; then
	echo "## Skipped tests from [snapd-testing-skip](https://github.com/canonical/snapd-testing-skip)" >>report
	echo "*If you wish to have any of the below tests run in your PR, in your PR description, add 'unskip:' followed by a copy-and-pasted list of the below tests you wish to run (unskip plus test list must be valid yaml)*" >>report
	find . -name skipped_tests_list.txt -exec cat {} \; | tr ' ' '\n' | grep . | sed 's/:[^/:]*$//' | sort -u | awk '{print "- "$1}' >>report
fi

append_predictor_table() {
	local verb="$1"
	local heading="$2"

	if ! jq -e --arg verb "$verb" '.items[] | select(.success == false and .skipped != true and (.name // "") != "" and .verb == $verb and (.system // "") != "" and .start != null and .end != null)' consolidated-report.json >/dev/null; then
		return
	fi

	echo "### ${heading}" >>report
	echo "| Test | Retries | Predictor |" >>report
	echo "|------|---------|-----------|" >>report

	jq -r --arg verb "$verb" '
              [.items[]
                | select(.success == false and .skipped != true and (.name // "") != "" and .verb == $verb and (.system // "") != "" and .start != null and .end != null)
                | {
                    backend: (.backend // ""),
                    system: .system,
                    full_name: (if (.variant // "") != "" then "\(.name):\(.variant)" else .name end),
                    scenario: (.scenario // "generic")
                  }
              ]
              | sort_by([.backend, .system, .full_name, .scenario])
              | group_by([.backend, .system, .full_name, .scenario])
              | .[]
              | .[0] as $item
              | [
                  ((if $item.backend != "" then "\($item.backend):" else "" end) + "\($item.system):\($item.full_name)"),
                  ((length - 1) | tostring),
                  $item.full_name,
                  $item.system,
                  $item.scenario
                ]
              | @tsv
            ' consolidated-report.json |
		while IFS=$'\t' read -r display_name retries full_name system scenario; do
			response=$(curl -sf -G http://test-predictor.canonical.com:5000/predict \
				--max-time 10 \
				--data-urlencode "name=${full_name}" \
				--data-urlencode "verb=${verb}" \
				--data-urlencode "system=${system}" \
				--data-urlencode "scenario=${scenario}" \
				--data-urlencode "attempt=${workflow_run_attempt}" \
				2>/dev/null) || response='{}'
			probability=$(jq -r '.success_probability // empty' <<<"$response")
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

			echo "| ${display_name} | ${retries} | ${probability} |" >>report
		done

	echo "" >>report
}

if [ -f consolidated-report.json ] && jq -e '.items[] | select(.success == false and .skipped != true and (.name // "") != "" and (.verb // "") != "" and .verb != "checking" and (.system // "") != "" and .start != null and .end != null)' consolidated-report.json >/dev/null; then
	echo "## Test Predictor Analysis" >>report
	append_predictor_table "preparing" "Preparing"
	append_predictor_table "executing" "Executing"
	append_predictor_table "restoring" "Restoring"
fi

# display the report
grep -n '' report
