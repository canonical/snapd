#!/usr/bin/make -f

.PHONY: all
all:
	make -C src
	make -C tests

%:
	make -C src $@
	make -C tests $@

.PHONY: syntax-check
syntax-check:
	make -C src syntax-check

.PHONY: shell-check
shell-check:
	shellcheck --format=gcc tests/test_* tests/*.sh

.PHONY: check
check: all syntax-check shell-check
	make -C tests test
