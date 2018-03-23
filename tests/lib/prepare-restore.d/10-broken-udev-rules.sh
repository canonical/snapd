#!/bin/sh

# Udev rules are notoriously hard to write and seemingly correct but subtly
# wrong rules can pass review. Whenever that happens udev logs an error
# message. As a last resort from lack of a better mechanism we can try to pick
# up such errors.

on_restore_project_each() {
	if grep "invalid .*snap.*.rules" /var/log/syslog; then
		echo "Invalid udev file detected, test most likely broke it"
		exit 1
	fi
}
