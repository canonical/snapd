SUBDIRS = src tests docs
EXTRA_DIST = PORTING README.md VERSION

.PHONY: check
check: check-syntax

.PHONY: check-syntax
check-syntax:
	shellcheck --format=gcc spread-tests/spread-prepare.sh
