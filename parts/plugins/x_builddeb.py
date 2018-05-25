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

import glob
import os

import snapcraft

# cowboy baby, see https://bugs.launchpad.net/snapcraft/+bug/1772584
def patch_snapcraft():
    import snapcraft.internal.common
    # very hacky but gets the job done for now, right now
    # SNAPCRAFT_FILES is only used to know what to exclude
    snapcraft.internal.common.SNAPCRAFT_FILES.remove("snap")
patch_snapcraft()



class XBuildDeb(snapcraft.BasePlugin):

    def build(self):
        self.run(["sudo", "apt-get", "build-dep", "-y", "./"])
        # XXX: get this from "debian/gbp.conf:postexport"
        self.run(["./get-deps.sh"])
        # run the real build, -ptrue means run "true" to sign the package.
        # Newer dpkg-buildpackage has "--no-sign" but not the xenial version
        self.run(["dpkg-buildpackage", "-ptrue"])
        # and "install" into the right place
        snapd_deb = glob.glob("../snapd_*.deb")[0]
        self.run(["dpkg-deb", "-x", os.path.abspath(snapd_deb), self.installdir])

