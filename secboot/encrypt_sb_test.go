// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot
// +build !nosecboot

/*
 * Copyright (C) 2022 Canonical Ltd
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

package secboot_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	sb "github.com/snapcore/secboot"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/snapdtool"
	"github.com/snapcore/snapd/testutil"
)

func (s *encryptSuite) TestFormatEncryptedDevice(c *C) {
	for _, tc := range []struct {
		initErr error
		err     string
	}{
		{initErr: nil, err: ""},
		{initErr: errors.New("some error"), err: "some error"},
	} {
		// create empty key to prevent blocking on lack of system entropy
		myKey := keys.EncryptionKey{}
		for i := range myKey {
			myKey[i] = byte(i)
		}

		calls := 0
		restore := secboot.MockSbInitializeLUKS2Container(func(devicePath, label string, key []byte,
			opts *sb.InitializeLUKS2ContainerOptions) error {
			calls++
			c.Assert(devicePath, Equals, "/dev/node")
			c.Assert(label, Equals, "my label")
			c.Assert(key, DeepEquals, []byte(myKey))
			c.Assert(opts, DeepEquals, &sb.InitializeLUKS2ContainerOptions{
				MetadataKiBSize:     2048,
				KeyslotsAreaKiBSize: 2560,
				KDFOptions: &sb.KDFOptions{
					MemoryKiB:       32,
					ForceIterations: 4,
				},
			})
			return tc.initErr
		})
		defer restore()

		err := secboot.FormatEncryptedDevice(myKey, "my label", "/dev/node")
		c.Assert(calls, Equals, 1)
		if tc.err == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, ErrorMatches, tc.err)
		}
	}
}

type keymgrSuite struct {
	testutil.BaseTest

	d             string
	keymgrCmd     *testutil.MockCmd
	udevadmCmd    *testutil.MockCmd
	systemdRunCmd *testutil.MockCmd
}

var _ = Suite(&keymgrSuite{})

func (s *keymgrSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.d = c.MkDir()
	s.systemdRunCmd = testutil.MockCommand(c, "systemd-run", `
while true; do
    case "$1" in
        --*)
            shift
            ;;
        *)
            exec "$@"
            ;;
    esac
done
`)
	s.AddCleanup(s.systemdRunCmd.Restore)
	s.keymgrCmd = testutil.MockCommand(c, "snap-fde-keymgr", fmt.Sprintf(`
if [ "$1" = "change-encryption-key" ]; then
    cat > %s/input
fi
`, s.d))
	s.AddCleanup(s.keymgrCmd.Restore)

	s.udevadmCmd = testutil.MockCommand(c, "udevadm", `
	echo "ID_PART_ENTRY_UUID=something"
`)
	s.AddCleanup(s.udevadmCmd.Restore)

	restore := snapdtool.MockOsReadlink(func(string) (string, error) {
		return filepath.Join(filepath.Dir(s.keymgrCmd.Exe()), "snapd"), nil
	})
	s.AddCleanup(restore)
}

var (
	key = keys.EncryptionKey{'e', 'n', 'c', 'r', 'y', 'p', 't', 1, 1, 1, 1}
)

func (s *keymgrSuite) TestChangeEncryptionKeyHappy(c *C) {
	err := secboot.ChangeEncryptionKey("/dev/foo/bar", key)
	c.Assert(err, IsNil)
	c.Check(s.udevadmCmd.Calls(), DeepEquals, [][]string{
		{"udevadm", "info", "--query", "property", "--name", "/dev/foo/bar"},
	})
	c.Check(s.systemdRunCmd.Calls(), DeepEquals, [][]string{
		{
			"systemd-run",
			"--wait", "--pipe", "--collect", "--service-type=exec", "--quiet",
			"--property=KeyringMode=inherit", "--",
			s.keymgrCmd.Exe(), "change-encryption-key", "--device", "/dev/disk/by-partuuid/something",
		},
	})
	c.Check(s.keymgrCmd.Calls(), DeepEquals, [][]string{
		{"snap-fde-keymgr", "change-encryption-key", "--device", "/dev/disk/by-partuuid/something"},
	})
	var b bytes.Buffer
	json.NewEncoder(&b).Encode(struct {
		Key []byte `json:"key"`
	}{
		Key: key,
	})
	c.Check(filepath.Join(s.d, "input"), testutil.FileEquals, b.String())
}

func (s *keymgrSuite) TestChangeEncryptionKeyBadUdev(c *C) {
	udevadmCmd := testutil.MockCommand(c, "udevadm", `
	echo "unhappy udev"
`)
	defer udevadmCmd.Restore()
	err := secboot.ChangeEncryptionKey("/dev/foo/bar", key)
	c.Assert(err, ErrorMatches, "cannot get UUID of partition /dev/foo/bar: cannot get required udev partition UUID property")
	c.Check(udevadmCmd.Calls(), DeepEquals, [][]string{
		{"udevadm", "info", "--query", "property", "--name", "/dev/foo/bar"},
	})
	c.Check(s.systemdRunCmd.Calls(), HasLen, 0)
	c.Check(s.keymgrCmd.Calls(), HasLen, 0)
}

func (s *keymgrSuite) TestChangeEncryptionKeyBadKeymgr(c *C) {
	keymgrCmd := testutil.MockCommand(c, "snap-fde-keymgr", `echo keymgr very unhappy; exit 1`)
	defer keymgrCmd.Restore()
	// update where /proc/self/exe resolves to
	restore := snapdtool.MockOsReadlink(func(string) (string, error) {
		return filepath.Join(filepath.Dir(keymgrCmd.Exe()), "snapd"), nil
	})
	defer restore()

	err := secboot.ChangeEncryptionKey("/dev/foo/bar", key)
	c.Assert(err, ErrorMatches, "cannot run FDE key manager tool: cannot run .*: keymgr very unhappy")

	c.Check(s.udevadmCmd.Calls(), DeepEquals, [][]string{
		{"udevadm", "info", "--query", "property", "--name", "/dev/foo/bar"},
	})
	c.Check(s.systemdRunCmd.Calls(), DeepEquals, [][]string{
		{
			"systemd-run",
			"--wait", "--pipe", "--collect", "--service-type=exec", "--quiet",
			"--property=KeyringMode=inherit", "--",
			keymgrCmd.Exe(), "change-encryption-key", "--device", "/dev/disk/by-partuuid/something",
		},
	})
	c.Check(keymgrCmd.Calls(), DeepEquals, [][]string{
		{"snap-fde-keymgr", "change-encryption-key", "--device", "/dev/disk/by-partuuid/something"},
	})
}
