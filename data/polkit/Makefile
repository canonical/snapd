#
# Copyright (C) 2024 Canonical Ltd
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

DATADIR = /usr/share
POLKITACTIONSDIR := $(DATADIR)/polkit-1/actions

POLKIT_ACTIONS = $(wildcard *.policy)

.PHONY: all
all: $(POLKIT_ACTIONS)

.PHONY: install
install: $(POLKIT_ACTIONS)
	install -d -m 0755 $(DESTDIR)/$(POLKITACTIONSDIR)
	install -m 0644 -t $(DESTDIR)/$(POLKITACTIONSDIR) $^

.PHONY: clean
clean:
	echo "Nothing to see here."
