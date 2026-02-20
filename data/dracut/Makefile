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

DRACUT_DIR := /usr/lib/dracut
DRACUT_CONF_DIR := $(DRACUT_DIR)/dracut.conf.d

DRACUT_CONF_FILES = $(wildcard *.conf)

.PHONY: all
all: $(DRACUT_CONF_FILES)

.PHONY: install
install: $(DRACUT_CONF_FILES)
	install -d -m 0755 $(DESTDIR)/$(DRACUT_CONF_DIR)
	install -m 0644 -t $(DESTDIR)/$(DRACUT_CONF_DIR) $^

.PHONY: clean
clean:
	echo "Nothing to see here."
