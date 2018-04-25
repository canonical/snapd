#!/bin/bash

# Define a dummy MATCH and REBOOT functions. This file is replaced by real
# functions, as defined by spread, on project prepare (in spread.yaml).

MATCH() {
	echo "dummy MATCH function invoked"
	false
}

REBOOT() {
	echo "dummy REBOOT function invoked"
	false
}
