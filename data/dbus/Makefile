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

include ../../build-config

DBUSSERVICESDIR = $(shell pkg-config --variable=session_bus_services_dir dbus-1)

SERVICES_GENERATED := $(patsubst %.service.in,%.service,$(wildcard *.service.in))
SERVICES := ${SERVICES_GENERATED}

%.service: %.service.in
	cat $< | sed 's:@bindir@:${bindir}:g' | cat > $@

all: ${SERVICES}

install: ${SERVICES}
	# NOTE: old (e.g. 14.04) GNU coreutils doesn't -D with -t
	install -d -m 0755 ${DESTDIR}/${DBUSSERVICESDIR}
	install -m 0644 -t ${DESTDIR}/${DBUSSERVICESDIR} $^

clean:
	rm -f ${SERVICES_GENERATED}
