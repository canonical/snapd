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

dbusservicesdir = $(DBUS_SESSION_SERVICES_DIR)

SERVICES = \
	io.snapcraft.Launcher.service \
	io.snapcraft.Settings.service

dbusservices_DATA = $(SERVICES)

%.service: %.service.in
	sed < $< > $@ \
		-e s:[@]bindir[@]:${bindir}:g

CLEANFILES = $(SERVICES)
