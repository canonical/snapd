TARGETS?= snappy
SHARE?=/usr/share
MODULES?=${TARGETS:=.pp.bz2}

all: ${TARGETS:=.pp.bz2}

%.pp.bz2: %.pp
	@echo Compressing $^ -\ $@
	bzip2 -9 $^

%.pp: %.te
	make -f ${SHARE}/selinux/devel/Makefile $@

clean:
	rm -f *~ *.tc *.pp *.pp.bz2
	rm -rf tmp

