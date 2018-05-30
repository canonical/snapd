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

SNAP_MOUNT_DIR ?= /snap
SYSTEMD_ENV_GENERATORS_DIR ?= /usr/lib/systemd/systemd-environment-generators/

SYSTEMD_ENV_GENERATORS_IN := $(wildcard *.in)
SYSTEMD_ENV_GENERATORS = $(SYSTEMD_ENV_GENERATORS_IN:.in=)

.PHONY: all
all: $(SYSTEMD_ENV_GENERATORS)

.PHONY: install
install: $(SYSTEMD_ENV_GENERATORS)
	# NOTE: old (e.g. 14.04) GNU coreutils doesn't -D with -t
	install -d -m 0755 $(DESTDIR)/$(SYSTEMD_ENV_GENERATORS_DIR)
	install -m 0755 -t $(DESTDIR)/$(SYSTEMD_ENV_GENERATORS_DIR) $^

.PHONY: clean
clean:
	rm -f $(SYSTEMD_ENV_GENERATORS_IN:.in=)

%.sh: %.sh.in
	cat $< | \
		sed s:@SNAP_MOUNT_DIR@:$(SNAP_MOUNT_DIR):g  | \
		cat > $@
