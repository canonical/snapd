#!/bin/sh
#
# Copyright (C) 2015-2019 Canonical Ltd
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

DEBUG=false

if [ -e "$SNAP_DATA/config" ]; then
	# shellcheck source=/dev/null
	. "$SNAP_DATA/config"
fi

UPOWER_OPTS=
if [ $DEBUG = true ]; then
	UPOWER_OPTS="-v"
fi

mkdir -p "$SNAP_COMMON/history"

if [ ! -e "$SNAP_DATA/UPower.conf" ]; then
	cp "$SNAP/etc/UPower/UPower.conf" "$SNAP_DATA"
fi

export UPOWER_CONF_FILE_NAME="$SNAP_DATA"/UPower.conf

exec "$SNAP/usr/libexec/upowerd" $UPOWER_OPTS
