#!/usr/bin/make -f

all:
	make -C src
	make -C tests

%:
	make -C src $@
	make -C tests $@

syntax-check:
	make -C src syntax-check

shell-check:
	shellcheck --format=gcc tests/test_* tests/*.sh

check: all syntax-check shell-check
	make -C tests test
