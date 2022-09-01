#!/bin/bash

# This script is used to setup the path and other environment variables needed
# to run the tools when the tools aren't used as part of a spread test.
# The steps are:
# 1. source the file: `. <PATH_TO_PROJECT>/setup.sh`
# 2. run any tool: `retry true`

PROJECT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"

export PATH=$PATH:$PROJECT_DIR/tools:$PROJECT_DIR/utils:$PROJECT_DIR/remote
