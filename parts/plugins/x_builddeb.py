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

# cowboy baby :(
# see https://bugs.launchpad.net/snapcraft/+bug/1772584
# and https://bugs.launchpad.net/snapcraft/+bug/1791871
#
# This will be fixed by snapcraft - the agreement is that they
# will look for a .snap/ directory with "snapcraft" in it and
# use that when available.
def patch_snapcraft():
    import snapcraft.internal.common
    import snapcraft.internal.sources._local
    # very hacky but gets the job done for now, right now
    # SNAPCRAFT_FILES is only used to know what to exclude
    snapcraft.internal.common.SNAPCRAFT_FILES.remove("snap")
    def _patched_check(self, target):
        return False
    snapcraft.internal.sources._local.Local._check = _patched_check
patch_snapcraft()



class XBuildDeb(snapcraft.BasePlugin):

    def build(self):
        super().build()
        self.run(["sudo", "apt-get", "build-dep", "-y", "./"])
        # ensure we have go in our PATH
        env=os.environ.copy()
        # ensure build with go-1.10 if available
        if os.path.exists("/usr/lib/go-1.10/bin"):
            env["PATH"] = "/usr/lib/go-1.10/bin:{}".format(env["PATH"])
        # XXX: get this from "debian/gbp.conf:postexport"
        self.run(["./get-deps.sh", "--skip-unused-check"], env=env)
        if os.getuid() == 0:
            # disable running the tests during the build when run as root
            # because quite a few of them will break
            env["DEB_BUILD_OPTIONS"] = "nocheck"
        # run the real build
        self.run(["dpkg-buildpackage"], env=env)
        # and "install" into the right place
        snapd_deb = glob.glob(os.path.join(self.partdir, "snapd_*.deb"))[0]
        self.run(["dpkg-deb", "-x", os.path.abspath(snapd_deb), self.installdir])

