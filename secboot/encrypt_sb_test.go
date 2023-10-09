// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot

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

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/snap/snaptest"
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

		err := secboot.FormatEncryptedDevice(myKey, secboot.EncryptionTypeLUKS, "my label", "/dev/node")
		c.Assert(calls, Equals, 1)
		if tc.err == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, ErrorMatches, tc.err)
		}
	}
}

func (s *encryptSuite) TestFormatEncryptedDeviceInvalidEncType(c *C) {
	err := secboot.FormatEncryptedDevice(keys.EncryptionKey{}, secboot.EncryptionType("other-enc-type"), "my label", "/dev/node")
	c.Check(err, ErrorMatches, `internal error: FormatEncryptedDevice for "/dev/node" expects a LUKS encryption type, not "other-enc-type"`)
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
set -e
if [ "$1" = "change-encryption-key" ]; then
    cat > %s/input
    exit 0
fi
if [ "$1" = "add-recovery-key" ]; then
    while true; do
        case "$1" in
            --key-file)
                shift
                printf "recovery11111111" > "$1"
                exit 0
                ;;
            *) shift ;;
        esac
    done
fi
if [ "$1" = "remove-recovery-key" ]; then
    while [ "$#" -gt 1 ]; do
        case "$1" in
            --key-file)
                shift
                rm -f "$1"
                ;;
            *) shift ;;
        esac
    done
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

func (s *keymgrSuite) TestStageEncryptionKeyHappy(c *C) {
	err := secboot.StageEncryptionKeyChange("/dev/foo/bar", key)
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
			"--stage",
		},
	})
	c.Check(s.keymgrCmd.Calls(), DeepEquals, [][]string{
		{"snap-fde-keymgr", "change-encryption-key", "--device", "/dev/disk/by-partuuid/something", "--stage"},
	})
	var b bytes.Buffer
	json.NewEncoder(&b).Encode(struct {
		Key []byte `json:"key"`
	}{
		Key: key,
	})
	c.Check(filepath.Join(s.d, "input"), testutil.FileEquals, b.String())
}

func (s *keymgrSuite) TestStageEncryptionKeyBadUdev(c *C) {
	udevadmCmd := testutil.MockCommand(c, "udevadm", `
	echo "unhappy udev"
`)
	defer udevadmCmd.Restore()
	err := secboot.StageEncryptionKeyChange("/dev/foo/bar", key)
	c.Assert(err, ErrorMatches, "cannot get UUID of partition /dev/foo/bar: cannot get required udev partition UUID property")
	c.Check(udevadmCmd.Calls(), DeepEquals, [][]string{
		{"udevadm", "info", "--query", "property", "--name", "/dev/foo/bar"},
	})
	c.Check(s.systemdRunCmd.Calls(), HasLen, 0)
	c.Check(s.keymgrCmd.Calls(), HasLen, 0)
}

func (s *keymgrSuite) TestStageTransitionEncryptionKeyBadKeymgr(c *C) {
	keymgrCmd := testutil.MockCommand(c, "snap-fde-keymgr", `echo keymgr very unhappy; exit 1`)
	defer keymgrCmd.Restore()
	// update where /proc/self/exe resolves to
	restore := snapdtool.MockOsReadlink(func(string) (string, error) {
		return filepath.Join(filepath.Dir(keymgrCmd.Exe()), "snapd"), nil
	})
	defer restore()

	err := secboot.StageEncryptionKeyChange("/dev/foo/bar", key)
	c.Assert(err, ErrorMatches, "cannot run FDE key manager tool: cannot run .*: keymgr very unhappy")

	c.Check(s.systemdRunCmd.Calls(), DeepEquals, [][]string{
		{
			"systemd-run",
			"--wait", "--pipe", "--collect", "--service-type=exec", "--quiet",
			"--property=KeyringMode=inherit", "--",
			keymgrCmd.Exe(), "change-encryption-key", "--device", "/dev/disk/by-partuuid/something",
			"--stage",
		},
	})
	c.Check(keymgrCmd.Calls(), DeepEquals, [][]string{
		{"snap-fde-keymgr", "change-encryption-key", "--device", "/dev/disk/by-partuuid/something", "--stage"},
	})

	s.systemdRunCmd.ForgetCalls()
	keymgrCmd.ForgetCalls()

	s.mocksForDeviceMounts(c)
	err = secboot.TransitionEncryptionKeyChange("/foo", key)
	c.Assert(err, ErrorMatches, "cannot run FDE key manager tool: cannot run .*: keymgr very unhappy")

	c.Check(s.systemdRunCmd.Calls(), DeepEquals, [][]string{
		{
			"systemd-run",
			"--wait", "--pipe", "--collect", "--service-type=exec", "--quiet",
			"--property=KeyringMode=inherit", "--",
			keymgrCmd.Exe(), "change-encryption-key", "--device", "/dev/disk/by-partuuid/foo-uuid",
			"--transition",
		},
	})
	c.Check(keymgrCmd.Calls(), DeepEquals, [][]string{
		{"snap-fde-keymgr", "change-encryption-key", "--device", "/dev/disk/by-partuuid/foo-uuid", "--transition"},
	})
}

func (s *keymgrSuite) TestTransitionEncryptionKeyNoMountDev(c *C) {
	restore := osutil.MockMountInfo(`
27 27 600:3 / /foo rw,relatime shared:7 - vfat /dev/mapper/foo rw
`[1:])
	s.AddCleanup(restore)

	udevadmCmd := testutil.MockCommand(c, "udevadm", `echo nope; exit 1`)
	defer udevadmCmd.Restore()

	err := secboot.TransitionEncryptionKeyChange("/foo", key)
	c.Assert(err, ErrorMatches, "cannot find matching device: cannot partition for mount /foo: cannot process udev properties of /dev/mapper/foo: nope")
}

func (s *keymgrSuite) TestTransitionEncryptionKeyHappy(c *C) {
	udevadmCmd := s.mocksForDeviceMounts(c)

	err := secboot.TransitionEncryptionKeyChange("/foo", key)
	c.Assert(err, IsNil)
	c.Check(udevadmCmd.Calls(), DeepEquals, [][]string{
		{"udevadm", "info", "--query", "property", "--name", "/dev/mapper/foo"},
		{"udevadm", "info", "--query", "property", "--name", "/dev/disk/by-uuid/5a522809-c87e-4dfa-81a8-8dc5667d1304"},
	})
	c.Check(s.systemdRunCmd.Calls(), DeepEquals, [][]string{
		{
			"systemd-run",
			"--wait", "--pipe", "--collect", "--service-type=exec", "--quiet",
			"--property=KeyringMode=inherit", "--",
			s.keymgrCmd.Exe(), "change-encryption-key", "--device", "/dev/disk/by-partuuid/foo-uuid",
			"--transition",
		},
	})
	c.Check(s.keymgrCmd.Calls(), DeepEquals, [][]string{
		{"snap-fde-keymgr", "change-encryption-key", "--device", "/dev/disk/by-partuuid/foo-uuid", "--transition"},
	})
	var b bytes.Buffer
	json.NewEncoder(&b).Encode(struct {
		Key []byte `json:"key"`
	}{
		Key: key,
	})
	c.Check(filepath.Join(s.d, "input"), testutil.FileEquals, b.String())
}

func (s *keymgrSuite) mocksForDeviceMounts(c *C) (udevadmCmd *testutil.MockCmd) {
	restore := osutil.MockMountInfo(`
27 27 600:3 / /foo rw,relatime shared:7 - vfat /dev/mapper/foo rw
27 27 600:4 / /bar rw,relatime shared:7 - vfat /dev/mapper/bar rw
`[1:])
	s.AddCleanup(restore)

	udevadmCmd = testutil.MockCommand(c, "udevadm", `
while [ "$#" -gt 1 ]; do
    case "$1" in
        --name)
            shift
            case "$1" in
                /dev/mapper/foo)
                    echo "DEVTYPE=disk"
                    echo "MAJOR=600"
                    echo "MINOR=3"
                    ;;
                /dev/mapper/bar)
                    echo "DEVTYPE=disk"
                    echo "MAJOR=600"
                    echo "MINOR=4"
                    ;;
                /dev/disk/by-uuid/5a522809-c87e-4dfa-81a8-8dc5667d1304)
                    echo "ID_PART_ENTRY_UUID=foo-uuid"
                    ;;
                /dev/disk/by-uuid/5a522809-c87e-4dfa-81a8-8dc5667d1305)
                    echo "ID_PART_ENTRY_UUID=bar-uuid"
                    ;;
            esac
            ;;
        *)
            shift
            ;;
    esac
done
`)
	s.AddCleanup(udevadmCmd.Restore)

	s.AddCleanup(func() { dirs.SetRootDir(dirs.GlobalRootDir) })
	dirs.SetRootDir(s.d)

	snaptest.PopulateDir(s.d, [][]string{
		{"/sys/dev/block/600:3/dm/uuid", "CRYPT-LUKS2-5a522809c87e4dfa81a88dc5667d1304-foo"},
		{"/sys/dev/block/600:3/dm/name", "foo"},
		{"/sys/dev/block/600:4/dm/uuid", "CRYPT-LUKS2-5a522809c87e4dfa81a88dc5667d1305-bar"},
		{"/sys/dev/block/600:4/dm/name", "bar"},
	})
	return udevadmCmd
}

func (s *keymgrSuite) TestEnsureRecoveryKey(c *C) {
	udevadmCmd := s.mocksForDeviceMounts(c)

	rkey, err := secboot.EnsureRecoveryKey(filepath.Join(s.d, "recovery.key"), []secboot.RecoveryKeyDevice{
		{Mountpoint: "/foo"},
		{Mountpoint: "/bar", AuthorizingKeyFile: "/authz/key.file"},
	})
	c.Assert(err, IsNil)
	c.Check(udevadmCmd.Calls(), DeepEquals, [][]string{
		{"udevadm", "info", "--query", "property", "--name", "/dev/mapper/foo"},
		{"udevadm", "info", "--query", "property", "--name", "/dev/disk/by-uuid/5a522809-c87e-4dfa-81a8-8dc5667d1304"},
		{"udevadm", "info", "--query", "property", "--name", "/dev/mapper/bar"},
		{"udevadm", "info", "--query", "property", "--name", "/dev/disk/by-uuid/5a522809-c87e-4dfa-81a8-8dc5667d1305"},
	})
	c.Check(s.systemdRunCmd.Calls(), DeepEquals, [][]string{
		{
			"systemd-run",
			"--wait", "--pipe", "--collect", "--service-type=exec", "--quiet",
			"--property=KeyringMode=inherit", "--",
			s.keymgrCmd.Exe(), "add-recovery-key",
			"--key-file", filepath.Join(s.d, "recovery.key"),
			"--devices", "/dev/disk/by-partuuid/foo-uuid",
			"--authorizations", "keyring",
			"--devices", "/dev/disk/by-partuuid/bar-uuid",
			"--authorizations", "file:/authz/key.file",
		},
	})
	c.Check(s.keymgrCmd.Calls(), DeepEquals, [][]string{
		{
			"snap-fde-keymgr", "add-recovery-key",
			"--key-file", filepath.Join(s.d, "recovery.key"),
			"--devices", "/dev/disk/by-partuuid/foo-uuid", "--authorizations", "keyring",
			"--devices", "/dev/disk/by-partuuid/bar-uuid", "--authorizations", "file:/authz/key.file",
		},
	})
	c.Check(rkey, DeepEquals, keys.RecoveryKey{'r', 'e', 'c', 'o', 'v', 'e', 'r', 'y', '1', '1', '1', '1', '1', '1', '1', '1'})
}

func (s *keymgrSuite) TestRemoveRecoveryKey(c *C) {
	udevadmCmd := s.mocksForDeviceMounts(c)

	snaptest.PopulateDir(s.d, [][]string{
		{"recovery.key", "foobar"},
	})
	// only one of the key files exists
	err := secboot.RemoveRecoveryKeys(map[secboot.RecoveryKeyDevice]string{
		{Mountpoint: "/foo"}: filepath.Join(s.d, "recovery.key"),
		{Mountpoint: "/bar", AuthorizingKeyFile: "/authz/key.file"}: filepath.Join(s.d, "missing-recovery.key"),
	})
	c.Assert(err, IsNil)

	expectedUdevCalls := [][]string{
		// order can change depending on map iteration
		{"udevadm", "info", "--query", "property", "--name", "/dev/mapper/foo"},
		{"udevadm", "info", "--query", "property", "--name", "/dev/disk/by-uuid/5a522809-c87e-4dfa-81a8-8dc5667d1304"},
		{"udevadm", "info", "--query", "property", "--name", "/dev/mapper/bar"},
		{"udevadm", "info", "--query", "property", "--name", "/dev/disk/by-uuid/5a522809-c87e-4dfa-81a8-8dc5667d1305"},
	}
	expectedSystemdRunCalls := [][]string{
		{
			"systemd-run",
			"--wait", "--pipe", "--collect", "--service-type=exec", "--quiet",
			"--property=KeyringMode=inherit", "--",
			s.keymgrCmd.Exe(), "remove-recovery-key",
			// order can change depending on map iteration
			"--devices", "/dev/disk/by-partuuid/foo-uuid", "--authorizations", "keyring",
			"--key-files", filepath.Join(s.d, "recovery.key"),
			"--devices", "/dev/disk/by-partuuid/bar-uuid", "--authorizations", "file:/authz/key.file",
			"--key-files", filepath.Join(s.d, "missing-recovery.key"),
		},
	}
	expectedKeymgrCalls := [][]string{
		{
			"snap-fde-keymgr", "remove-recovery-key",
			// order can change depending on map iteration
			"--devices", "/dev/disk/by-partuuid/foo-uuid", "--authorizations", "keyring",
			"--key-files", filepath.Join(s.d, "recovery.key"),
			"--devices", "/dev/disk/by-partuuid/bar-uuid", "--authorizations", "file:/authz/key.file",
			"--key-files", filepath.Join(s.d, "missing-recovery.key"),
		},
	}

	udevCalls := udevadmCmd.Calls()
	c.Assert(udevCalls, HasLen, len(expectedUdevCalls))
	// iteration order can be different though
	c.Assert(udevCalls[0], HasLen, 6)
	firstFoo := udevCalls[0][5] == "/dev/mapper/foo"

	if !firstFoo {
		// flip the order of foo and bar
		expectedUdevCalls = append(expectedUdevCalls[2:], expectedUdevCalls[0:2]...)
		expectedSystemdRunCalls[0] = append(expectedSystemdRunCalls[0][0:10],
			append(expectedSystemdRunCalls[0][16:], expectedSystemdRunCalls[0][10:16]...)...)
		expectedKeymgrCalls[0] = append(expectedKeymgrCalls[0][0:2],
			append(expectedKeymgrCalls[0][8:], expectedKeymgrCalls[0][2:8]...)...)
	}

	c.Check(s.systemdRunCmd.Calls(), DeepEquals, expectedSystemdRunCalls)
	c.Check(s.keymgrCmd.Calls(), DeepEquals, expectedKeymgrCalls)
}
