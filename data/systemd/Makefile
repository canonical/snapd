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

SNAPD_ENVIRONMENT_FILE := /etc/environment
SNAP_MOUNTDIR := /snap
BINDIR := /usr/bin
LIBEXECDIR := /usr/lib
SYSTEMDSYSTEMUNITDIR := /lib/systemd/system

SYSTEMD_UNITS_GENERATED := $(patsubst %.service.in,%.service,$(wildcard *.service.in))
SYSTEMD_UNITS := ${SYSTEMD_UNITS_GENERATED} snapd.refresh.timer snapd.socket

%.service: %.service.in
	cat $< | \
		sed s:@libexecdir@:${LIBEXECDIR}:g | \
		sed s:@SNAPD_ENVIRONMENT_FILE@:${SNAPD_ENVIRONMENT_FILE}:g | \
		sed s:@bindir@:${BINDIR}:g | \
		sed s:@SNAP_MOUNTDIR@:${SNAP_MOUNTDIR}:g  | \
		cat > $@

all: ${SYSTEMD_UNITS}

install: $(SYSTEMD_UNITS)
	install -d ${DESTDIR}/${SYSTEMDSYSTEMUNITDIR}
	for u in ${SYSTEMD_UNITS} ; do \
		install -m 0644 $$u ${DESTDIR}/${SYSTEMDSYSTEMUNITDIR}/ ; \
	done

clean:
	for u in ${SYSTEMD_UNITS_GENERATED} ; do \
		rm $$u ; \
	done
