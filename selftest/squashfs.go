// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package selftest

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"

	"compress/gzip"
	"encoding/base64"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/squashfs"
)

/* This image was created using:

#!/bin/sh

cd $(mktemp -d)
cat > canary.txt<<'EOF'
This file is used to check that snapd can read a squashfs image.

The squashfs was generated with:
EOF
cat $0 >> canary.txt

mksquashfs . /tmp/canary.squashfs -noappend -comp xz -no-xattrs -no-fragments >/dev/nul
cat /tmp/canary.squashfs | gzip - | base64

*/
var b64SquashfsImage = []byte(`
H4sIAGxdFVsAA8soLixmYmBgyIkVjWZgALEYGFgYBBkuMDECaQYGFQYI4INIMbBB6f9Q0MAI4R+D
0s+g9A8o/de8KiKKgYExU+meGfOB54wzmBUZuYDiVhPYbR4wTme4H8ugJcWpniK5waL4VLewwsUC
PgdVnS/pCycWn34g1rpj6bIywdLqaQdZFYQcYr7/vR1w9dTbDivRH3GahXc578hdW3Ri7mu9+KeF
PqYCrkk/5zepyFw0EjL+XxH/3ubc9E+/J0t0PxE+zv9J96pa0rt9CWyvX6aIvb3H65qo9mbikvjU
LZxrOupvcr32+2yYFzt1wTe2HdFfrOSmKXFFPf1i5ep7Wv+q+U+nBNWs/nu+UosO6PFvfl991nVG
R9XSJUxv/7/y2f2zid0+OnGi1+ey1/vatzDPvfbq+0LLwIu1Wx/u+m6/c8IN21vNCQwMX2dtWsHA
+BvodwaGpcmXftsZ8HaDg5ExMsqlgYlhCTisQDEAYiRAQxckNgMooADEjAwH4GqgEQCOK0UgBhrK
INcAFWRghMtyMiQn5iUWVeqVVJQIwOVh8QmLJ5aGF8wMsIgfBaNgFIyCUTAKRsEoGAWjYBSMglEw
bAEA+f+YuAAQAAA=
`)

func checkSquashfsMount() error {
	tmpSquashfsFile, err := ioutil.TempFile("", "selftest-squashfs-")
	if err != nil {
		return err
	}
	defer os.Remove(tmpSquashfsFile.Name())

	tmpMountDir, err := ioutil.TempDir("", "selftest-mountpoint-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpMountDir)

	// write the squashfs image
	b64dec := base64.NewDecoder(base64.StdEncoding, bytes.NewBuffer(b64SquashfsImage))
	gzReader, err := gzip.NewReader(b64dec)
	if err != nil {
		return err
	}
	if _, err := io.Copy(tmpSquashfsFile, gzReader); err != nil {
		return err
	}

	// the fstype can be squashfs or fuse.{snap,squash}fuse
	fstype, _, err := squashfs.FsType()
	if err != nil {
		return err
	}
	cmd := exec.Command("mount", "-t", fstype, tmpSquashfsFile.Name(), tmpMountDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("cannot mount squashfs image using %q: %v", fstype, osutil.OutputErr(output, err))
	}

	defer func() {
		if output, err := exec.Command("umount", tmpMountDir).CombinedOutput(); err != nil {
			// os.RemoveAll(tmpMountDir) will fail here if umount fails
			logger.Noticef("cannot unmount selftest squashfs image: %v", osutil.OutputErr(output, err))
		}
	}()

	// sanity check the
	content, err := ioutil.ReadFile(filepath.Join(tmpMountDir, "canary.txt"))
	if err != nil {
		return fmt.Errorf("squashfs mount returned no err but canary file cannot be read")
	}
	if !bytes.HasPrefix(content, []byte("This file is used to check that snapd can read a squashfs image.")) {
		return fmt.Errorf("unexpected squashfs canary content: %q", content)
	}

	return nil
}
