#
# Copyright (C) 2026 Canonical Ltd
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

DRACUTDIR := /usr/lib/dracut
SNAPDFILESDIR := $(DRACUTDIR)/dracut.conf.d/snapd

DRACUTFILES_CONF = $(wildcard *.conf)

.PHONY: all
all: $(DRACUTFILES_CONF)

.PHONY: install
install: $(DRACUTFILES_CONF)
	install -d -m 0755 $(DESTDIR)/$(SNAPDFILESDIR)
	install -m 0644 -t $(DESTDIR)/$(SNAPDFILESDIR) $^

.PHONY: clean
clean:
	echo "Nothing to see here."
