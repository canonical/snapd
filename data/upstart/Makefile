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

LIBEXECDIR ?= /usr/lib
UPSTARTSESSIONSDIR ?= /usr/share/upstart/sessions

UPSTART_SERVICES := $(patsubst %.conf.in,%.conf,$(wildcard *.conf.in))

%.conf: %.conf.in
	cat $< | sed "s:@libexecdir@:${LIBEXECDIR}:g" | cat > $@

all: ${UPSTART_SERVICES}

install: ${UPSTART_SERVICES}
	install -D -m 0644 -t ${DESTDIR}/${UPSTARTSESSIONSDIR} $^

clean:
	rm -f ${UPSTART_SERVICES}
