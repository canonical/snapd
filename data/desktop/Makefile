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
APPLICATIONSDIR = /usr/share/applications

DESKTOP_SOURCES = snap-handle-link.desktop.in
DESKTOP_FILES = $(DESKTOP_SOURCES:.in=)

AUTOSTART_SOURCES = snap-userd-autostart.desktop.in
AUTOSTART_FILES = $(AUTOSTART_SOURCES:.in=)

.PHONY: all
all: $(DESKTOP_FILES) $(AUTOSTART_FILES)

.PHONY: install
# NOTE: old (e.g. 14.04) GNU coreutils doesn't -D with -t
install:: $(DESKTOP_FILES)
	install -d -m 0755 $(DESTDIR)/$(APPLICATIONSDIR)
	install -m 0644 -t $(DESTDIR)/$(APPLICATIONSDIR) $^

install:: $(AUTOSTART_FILES)
	install -d -m 0755 $(DESTDIR)/$(SYSCONFXDGAUTOSTARTDIR)
	install -m 0644 -t $(DESTDIR)/$(SYSCONFXDGAUTOSTARTDIR) $^

.PHONY: clean
clean:
	rm -f $(DESKTOP_FILES) $(AUTOSTART_FILES)

%: %.in
	cat $< | \
		sed s:@bindir@:$(BINDIR):g | \
		cat > $@
