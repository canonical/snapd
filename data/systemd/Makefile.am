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

systemdsystemunitdir = $(SYSTEMD_SYSTEM_UNIT_DIR)

SYSTEMD_UNITS_GENERATED := \
	snapd.autoimport.service.in	\
	snapd.core-fixup.service.in \
	snapd.refresh.service.in \
	snapd.service.in \
	snapd.snap-repair.service.in \
	snapd.system-shutdown.service.in

SYSTEMD_UNITS = \
	$(SYSTEMD_UNITS_GENERATED:.in=) \
	$(wildcard *.timer) \
	$(wildcard *.socket) \
	$(snap_mount_unit)


CLEANFILES = $(SYSTEMD_UNITS_GENERATED:.in=) $(snap_mount_unit)

# The special mount unit for "/snap" must be named after the path and the path
# is a variable.  Compute it in make for simplicity. This is equivalent to
# calling systemd-escape --path $(SNAP_MOUNT_DIR)
snap_mount_unit=$(subst /,-,$(patsubst %/,%,$(patsubst /%,%,$(SNAP_MOUNT_DIR)))).mount

systemdsystemunit_DATA = $(SYSTEMD_UNITS)

libexec_SCRIPTS = snapd.core-fixup.sh

%: %.in
	cat $< | \
		sed s:[@]libexecdir[@]:$(libexecdir):g | \
		sed s:[@]SNAPD_ENVIRONMENT_FILE[@]:$(SNAPD_ENVIRONMENT_FILE):g | \
		sed s:[@]bindir[@]:$(bindir):g | \
		sed s:[@]SNAP_MOUNT_DIR[@]:$(SNAP_MOUNT_DIR):g  | \
		cat > $@

$(snap_mount_unit): snap.mount.in
	cat $< | \
		sed s:[@]SNAP_MOUNT_DIR[@]:$(SNAP_MOUNT_DIR):g  | \
		cat > $@
