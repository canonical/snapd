#!/bin/sh

set -e

./run-checks --unit

go tool cover -html=.coverage/coverage.out -o .coverage/coverage.html

echo "Coverage html reports are available in .coverage/coverage.html"
