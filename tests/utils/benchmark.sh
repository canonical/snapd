#!/bin/bash

BACKEND="${1:-linode:}"
ITERATIONS=${2:-10}
OUTPUT_FILE="${3:-${PWD}/benchmark.out}"
SUCCESSFUL_EXECUTIONS=0

rm -f "$OUTPUT_FILE"

for i in $(seq "$ITERATIONS"); do
    echo "Running iteration $i of $ITERATIONS"
    START_TIME=$SECONDS
    if spread -v "$BACKEND"; then
        SUCCESSFUL_EXECUTIONS=$((SUCCESSFUL_EXECUTIONS + 1))
        ITERATION_TIME=$((SECONDS - START_TIME))
        TOTAL_TIME=$((TOTAL_TIME + ITERATION_TIME))
        echo "$ITERATION_TIME" >> "$OUTPUT_FILE"
    fi
done
echo "$SUCCESSFUL_EXECUTIONS successful executions out of $ITERATIONS" >> "$OUTPUT_FILE"
echo "Average: $(echo "scale=2; $TOTAL_TIME / $SUCCESSFUL_EXECUTIONS" | bc)s" >> "$OUTPUT_FILE"
