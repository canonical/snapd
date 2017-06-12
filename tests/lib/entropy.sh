#!/bin/bash

generate_entropy() {
	# Regenerate entropy when the level is under the entropy used by ubuntu-core (700)
	if [ $# -gt 0 ] && [ "$1" = "--force" ] || [ $(sysctl --values kernel.random.entropy_avail || echo 0) -gt 700 ]; then
		echo "HRNGDEVICE=/dev/urandom" > /etc/default/rng-tools
		/etc/init.d/rng-tools restart
	fi
}
