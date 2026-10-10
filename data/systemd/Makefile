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
BINDIR := /usr/bin
LIBEXECDIR := /usr/lib
SYSTEMDSYSTEMUNITDIR := /lib/systemd/system

# Selects which snap-mount-dir unit to install: snap.mount (canonical
# /snap) or var-lib-snapd-snap.mount (/var/lib/snapd/snap). Alternate wins
# if both are set.
USE_CANONICAL_SNAP_MOUNT_DIR ?= true
USE_ALT_SNAP_MOUNT_DIR ?= false
ifeq ($(USE_ALT_SNAP_MOUNT_DIR),true)
SNAP_MOUNT_DIR_UNIT := var-lib-snapd-snap.mount
else
SNAP_MOUNT_DIR_UNIT := snap.mount
endif

# NOTE: the code in wrappers/core18.go assumes that all our service units's
# name start with the "snapd." prefix; units whose filename does not have this
# prefix will not be installed on the host! SNAP_MOUNT_DIR_UNIT and
# var-snap.mount rely on this to stay classic-only.
SYSTEMD_UNITS_GENERATED := $(wildcard *.in)
# NOTE: sort removes duplicates so this gives us all the units, generated or otherwise
SYSTEMD_UNITS = $(sort $(SYSTEMD_UNITS_GENERATED:.in=) $(wildcard *.service) $(wildcard *.timer) $(wildcard *.socket) $(wildcard *.target) $(SNAP_MOUNT_DIR_UNIT) var-snap.mount)

.PHONY: all
all: $(SYSTEMD_UNITS)

.PHONY: check
check: snapd.run-from-snap snapd.core-fixup.sh
	if command -v shellcheck >/dev/null; then shellcheck $^; fi

.PHONY: install
install: $(SYSTEMD_UNITS)
	# NOTE: old (e.g. 14.04) GNU coreutils doesn't -D with -t
	install -d -m 0755 $(DESTDIR)/$(SYSTEMDSYSTEMUNITDIR)
	install -m 0644 -t $(DESTDIR)/$(SYSTEMDSYSTEMUNITDIR) $^
	install -d -m 0755 $(DESTDIR)/$(LIBEXECDIR)/snapd
	install -m 0755 -t $(DESTDIR)/$(LIBEXECDIR)/snapd snapd.core-fixup.sh
	install -m 0755 -t $(DESTDIR)/$(LIBEXECDIR)/snapd snapd.run-from-snap

.PHONY: clean
clean:
	rm -f $(SYSTEMD_UNITS_GENERATED:.in=)

%: %.in
	cat $< | \
		sed s:@libexecdir@:$(LIBEXECDIR):g | \
		sed s:@SNAPD_ENVIRONMENT_FILE@:$(SNAPD_ENVIRONMENT_FILE):g | \
		sed s:@bindir@:$(BINDIR):g | \
		cat > $@
