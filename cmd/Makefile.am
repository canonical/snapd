SUBDIRS = snap-confine
EXTRA_DIST = VERSION

.PHONY: check-syntax
check-syntax:
	shellcheck --format=gcc snap-confine/spread-tests/spread-prepare.sh
