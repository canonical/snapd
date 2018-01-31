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

%.sh: %.sh.in
	sed   < $< > $@ \
		s:[@]SNAP_MOUNT_DIR[@]:${SNAP_MOUNT_DIR}:g

CLEANFILES = snapd.sh

envprofiledir = $(sysconfdir)/profile.d

envprofile_DATA = snapd.sh
