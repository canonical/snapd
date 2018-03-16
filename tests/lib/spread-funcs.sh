#!/bin/bash

# Define a dummy MATCH function. This file is replaced by the real MATCH
# function, as defined by spread, on project prepare (in spread.yaml).
#
MATCH() {
	echo "dummy MATCH function invoked"
	false
}

