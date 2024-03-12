// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package osutil_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
)

type nfsSuite struct{}

var _ = Suite(&nfsSuite{})

func (s *nfsSuite) TestIsHomeUsingNFS(c *C) {
	cases := []struct {
		mountinfo, fstab string
		nfs              bool
		errorPattern     string
	}{{
		// Errors from parsing mountinfo and fstab are propagated.
		mountinfo:    "bad syntax",
		errorPattern: "cannot parse mountinfo:.*, .*",
	}, {
		fstab:        "bad syntax",
		errorPattern: "cannot parse .*/fstab.*, .*",
	}, {
		// NFSv3 {tcp,udp} and NFSv4 currently mounted at /home/zyga/nfs are recognized.
		mountinfo: "1074 28 0:59 / /home/zyga/nfs rw,relatime shared:342 - nfs localhost:/srv/nfs rw,vers=3,rsize=1048576,wsize=1048576,namlen=255,hard,proto=tcp,timeo=600,retrans=2,sec=sys,mountaddr=127.0.0.1,mountvers=3,mountport=54125,mountproto=tcp,local_lock=none,addr=127.0.0.1",
		nfs:       true,
	}, {
		mountinfo: "1074 28 0:59 / /home/zyga/nfs rw,relatime shared:342 - nfs localhost:/srv/nfs rw,vers=3,rsize=32768,wsize=32768,namlen=255,hard,proto=udp,timeo=11,retrans=3,sec=sys,mountaddr=127.0.0.1,mountvers=3,mountport=47875,mountproto=udp,local_lock=none,addr=127.0.0.1",
		nfs:       true,
	}, {
		mountinfo: "680 27 0:59 / /home/zyga/nfs rw,relatime shared:478 - nfs4 localhost:/srv/nfs rw,vers=4.2,rsize=524288,wsize=524288,namlen=255,hard,proto=tcp,port=0,timeo=600,retrans=2,sec=sys,clientaddr=127.0.0.1,local_lock=none,addr=127.0.0.1",
		nfs:       true,
	}, {
		// NFSv3 {tcp,udp} and NFSv4 currently mounted at /home/zyga/nfs are ignored (not in $HOME).
		mountinfo: "1074 28 0:59 / /mnt/nfs rw,relatime shared:342 - nfs localhost:/srv/nfs rw,vers=3,rsize=1048576,wsize=1048576,namlen=255,hard,proto=tcp,timeo=600,retrans=2,sec=sys,mountaddr=127.0.0.1,mountvers=3,mountport=54125,mountproto=tcp,local_lock=none,addr=127.0.0.1",
	}, {
		mountinfo: "1074 28 0:59 / /mnt/nfs rw,relatime shared:342 - nfs localhost:/srv/nfs rw,vers=3,rsize=32768,wsize=32768,namlen=255,hard,proto=udp,timeo=11,retrans=3,sec=sys,mountaddr=127.0.0.1,mountvers=3,mountport=47875,mountproto=udp,local_lock=none,addr=127.0.0.1",
	}, {
		mountinfo: "680 27 0:59 / /mnt/nfs rw,relatime shared:478 - nfs4 localhost:/srv/nfs rw,vers=4.2,rsize=524288,wsize=524288,namlen=255,hard,proto=tcp,port=0,timeo=600,retrans=2,sec=sys,clientaddr=127.0.0.1,local_lock=none,addr=127.0.0.1",
	}, {
		// NFS that may be mounted at /home and /home/zyga/nfs is recognized.
		// Two spellings are possible, "nfs" and "nfs4" (they are equivalent
		// nowadays).
		fstab: "localhost:/srv/nfs /home nfs defaults 0 0",
		nfs:   true,
	}, {
		fstab: "localhost:/srv/nfs /home nfs4 defaults 0 0",
		nfs:   true,
	}, {
		fstab: "localhost:/srv/nfs /home/zyga/nfs nfs defaults 0 0",
		nfs:   true,
	}, {
		fstab: "localhost:/srv/nfs /home/zyga/nfs nfs4 defaults 0 0",
		nfs:   true,
	}, {
		// NFS that may be mounted at /mnt/nfs is ignored (not in $HOME).
		fstab: "localhost:/srv/nfs /mnt/nfs nfs defaults 0 0",
	}, {
		// autofs that is mounted at /home.
		mountinfo: "137 29 0:50 / /home rw,relatime shared:87 - autofs /etc/auto.master.d/home rw,fd=7,pgrp=22588,timeout=300,minproto=5,maxproto=5,indirect,pipe_ino=173399",
		nfs:       true,
	}, {
		// cifs that is mounted at /home
		// This is not real data, it is made-up.
		mountinfo: "0 0 0:0 / /home rw,relatime shared:0 - cifs //sub.example.org/path$/all-users irrelevant-options",
		nfs:       true,
	}, {
		// cifs that is mounted at /home/$USERNAME
		// This is not real data, it is made-up.
		mountinfo: "0 0 0:0 / /home/some-user rw,relatime shared:0 - cifs //sub.example.org/path$/some-user irrelevant-options",
		nfs:       true,
	}}
	for _, tc := range cases {
		restore := osutil.MockMountInfo(tc.mountinfo)
		defer restore()
		restore = osutil.MockEtcFstab(tc.fstab)
		defer restore()

		nfs, err := osutil.IsHomeUsingNFS()
		if tc.errorPattern != "" {
			c.Assert(err, ErrorMatches, tc.errorPattern, Commentf("test case %#v", tc))
		} else {
			c.Assert(err, IsNil)
		}
		c.Assert(nfs, Equals, tc.nfs)
	}
}
