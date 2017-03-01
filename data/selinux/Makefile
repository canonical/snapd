# This file is part of snapd-selinux
# Copyright (C) 2016 Neal Gompa
#
# This program is free software; you can redistribute it and/or modify
# it under the terms of the GNU General Public License as published by
# the Free Software Foundation; either version 2 of the License, or
# (at your option) any later version.
#
# This program is distributed in the hope that it will be useful,
# but WITHOUT ANY WARRANTY; without even the implied warranty of
# MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
# GNU Library General Public License for more details.
#
# You should have received a copy of the GNU General Public License
# along with this program; if not, write to the Free Software
# Foundation, Inc., 51 Franklin Street, Fifth Floor, Boston, MA 02110-1301 USA.


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

