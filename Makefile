#!/usr/bin/make -f

all:
	make -C src

%:
	make -C src $@

