#!/bin/bash

feed_kernel_entropy_pool() {
	# Feed the kernel entropy pool when the level is under 700 (level used by ubuntu-core)
	if [ $# -gt 0 ] && [ "$1" = "--force" ] || [ $(sysctl --values kernel.random.entropy_avail || echo 0) -gt 700 ]; then
		echo "HRNGDEVICE=/dev/urandom" > /etc/default/rng-tools
		/etc/init.d/rng-tools restart
	fi
}
