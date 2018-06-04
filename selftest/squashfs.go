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

$ cat > canary.txt<<'EOF'
This file is used to check that snapd can read a squashfs image.

The squashfs was generated with:
$ mksquashfs . /tmp/canary.squashfs -noappend -comp xz -no-xattrs -no-fragments
EOF

$ mksquashfs . /tmp/canary.squashfs -noappend -comp xz -no-xattrs -no-fragments
$ cat /tmp/canary.squashfs | gzip - | base64

*/
var b64SquashfsImage = []byte(`
H4sIABNhDVsAA+2QsU7DMBCGL6ELQUggMaND6pp0Z+YROsHCKXHiqNhJY1cNnbLDyMSIeALEg/AY
LDwDOI0THqDrfdLpP/93PtknzdqEAPBxcnoH0GcAMziHrzBwCjCHgbdg0Fevv54Lf771uvP67HUp
S4N5+SDQ6caIDG2FqRTpCq0ki0ZTnWFKGhtBGRKa9YaMzA2WigqRRNFSin9zSwYLoUVD1o3allZe
R3NUq6khwYVV9cINpOYxmexYV1TXQmcYp5Wqsd31VtyStc2+GucNFUpoa6Lopgvhff/+J7eVPrn3
P+69zyGCAH6mntBvqdcrF0cuLjtvQjBVj8E/zLb2bKqPOx53N+u+3YCX8RrDMAzDMAzDMAzDHMwf
BbTZHgAQAAA=
`)

func trySquashfsMount() error {
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
			// os.RemoveAll(tmpMountDir) will fail too
			logger.Noticef("cannot unmount selftest squashfs image: %v", osutil.OutputErr(output, err))
		}
	}()

	// sanity check the
	content, err := ioutil.ReadFile(filepath.Join(tmpMountDir, "canary.txt"))
	if err != nil {
		return fmt.Errorf("squashfs mount returned no err but canary file cannot be read")
	}
	if !bytes.Equal(content, []byte("This file is used to check that snapd can read a squashfs image.\n")) {
		return fmt.Errorf("unexpected squashfs canary content: %q", content)
	}

	return nil
}
