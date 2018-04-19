#
# Copyright (C) 2018 Canonical Ltd
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

BINDIR = /usr/bin
SYSCONFXDGAUTOSTARTDIR = /etc/xdg/autostart

SOURCES = snap-userd-autostart.desktop.in
DESKTOP_FILES = $(SOURCES:.in=)

.PHONY: all
all: $(DESKTOP_FILES)

.PHONY: install
install: $(DESKTOP_FILES)
	# NOTE: old (e.g. 14.04) GNU coreutils doesn't -D with -t
	install -d -m 0755 $(DESTDIR)/$(SYSCONFXDGAUTOSTARTDIR)
	install -m 0644 -t $(DESTDIR)/$(SYSCONFXDGAUTOSTARTDIR) $^

.PHONY: clean
clean:
	rm -f $(DESKTOP_FILES)

%: %.in
	cat $< | \
		sed s:@bindir@:$(BINDIR):g | \
		cat > $@
