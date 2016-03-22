#!/usr/bin/make -f

all:
	make -C src
	make -C tests

%:
	make -C src $@
	make -C tests $@

syntax-check:
	make -C src syntax-check

check: all syntax-check
	make -C tests test
