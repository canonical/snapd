SUBDIRS = cmd/snap-confine cmd/snap-confine/tests docs
EXTRA_DIST = VERSION

.PHONY: check-syntax
check-syntax:
	shellcheck --format=gcc cmd/snap-confine/spread-tests/spread-prepare.sh
