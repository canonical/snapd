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

class XBuildDeb(snapcraft.BasePlugin):

    def build(self):
        super().build()
        # ensure we have go in our PATH
        env=os.environ.copy()
        env["DEBIAN_FRONTEND"] = "noninteractive"
        env["DEBCONF_NONINTERACTIVE_SEEN"] = "true"
        self.run(["apt-get", "build-dep", "-y", "./"], env=env)
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

