SUBDIRS = cmd/snap-confine
EXTRA_DIST = VERSION

.PHONY: check-syntax
check-syntax:
	shellcheck --format=gcc cmd/snap-confine/spread-tests/spread-prepare.sh
