#
# Copyright (C) 2017 Canonical Ltd
#
# This program is free software: you can redistribute it and/or modify
# it under the terms of the GNU General Public License version 3 as
# published by the Free Software Foundation.
#
# This program is distributed in the hope that it will be useful,
# but WITHOUT ANY WARRANTY; without even the implied warranty of
# MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
# GNU General Public License for more details.
#
# You should have received a copy of the GNU General Public License
# along with this program.  If not, see <http://www.gnu.org/licenses/>.

SNAP_MOUNT_DIR := /snap
ENVD := /etc/profile.d
DATADIR ?= /usr/share

%.sh: %.sh.in
	sed   < $< > $@ \
		s:@SNAP_MOUNT_DIR@:${SNAP_MOUNT_DIR}:g

%.fish: %.fish.in
	sed   < $< > $@ \
		s:@SNAP_MOUNT_DIR@:${SNAP_MOUNT_DIR}:g

GENERATED_SH = snapd.sh
GENERATED_FISH = snapd.fish
GENERATED = ${GENERATED_SH} ${GENERATED_FISH}

all: ${GENERATED}
.PHONY: all

install-sh: ${GENERATED_SH}
	# NOTE: old (e.g. 14.04) GNU coreutils doesn't -D with -t
	install -d -m 0755 ${DESTDIR}/${ENVD}
	install -m 0644 -t ${DESTDIR}/${ENVD} $^

# fish uses a separate directory to store vendor configuration files
install-fish: ${GENERATED_FISH}
	install -d -m 0755 ${DESTDIR}/${DATADIR}/fish/vendor_conf.d
	install -m 0644 -t ${DESTDIR}/${DATADIR}/fish/vendor_conf.d $^

install: install-sh install-fish
.PHONY: install install-sh install-fish

clean:
	$(RM) ${GENERATED}
.PHONY: clean
