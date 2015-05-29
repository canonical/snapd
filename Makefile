#!/usr/bin/make -f

all:
	make -C src
#	make -C tests

%:
	make -C src $@
#	make -C tests $@

check: all
	make -C tests test
