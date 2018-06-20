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

%.sh: %.sh.in
	sed   < $< > $@ \
		s:@SNAP_MOUNT_DIR@:${SNAP_MOUNT_DIR}:g

GENERATED = snapd.sh


all: ${GENERATED}
.PHONY: all

install: ${GENERATED}
	# NOTE: old (e.g. 14.04) GNU coreutils doesn't -D with -t
	install -d -m 0755 ${DESTDIR}/${ENVD}
	install -m 0644 -t ${DESTDIR}/${ENVD} $^
.PHONY: install

clean:
	$(RM) ${GENERATED}
.PHONY: clean
