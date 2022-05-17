// -*- Mode: Go; indent-tabs-mode: t -*-

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
package keymgr_test

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	sb "github.com/snapcore/secboot"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/secboot/keymgr"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/secboot/luks2"
	"github.com/snapcore/snapd/testutil"
)

type keymgrSuite struct {
	testutil.BaseTest

	rootDir       string
	cryptsetupCmd *testutil.MockCmd
}

var _ = Suite(&keymgrSuite{})

func TestT(t *testing.T) {
	TestingT(t)
}

const mockedMeminfo = `MemTotal:         929956 kB
CmaTotal:         131072 kB
`

var mockRecoveryKey = keys.RecoveryKey{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}

func (s *keymgrSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.cryptsetupCmd = testutil.MockCommand(c, "cryptsetup", ``)
	s.AddCleanup(s.cryptsetupCmd.Restore)
	s.rootDir = c.MkDir()
	dirs.SetRootDir(s.rootDir)
	s.AddCleanup(func() { dirs.SetRootDir("/") })
	c.Assert(os.MkdirAll(dirs.RunDir, 0755), IsNil)

	mockedMeminfoFile := filepath.Join(c.MkDir(), "meminfo")
	err := ioutil.WriteFile(mockedMeminfoFile, []byte(mockedMeminfo), 0644)
	c.Assert(err, IsNil)
	s.AddCleanup(osutil.MockProcMeminfo(mockedMeminfoFile))
}

func (s *keymgrSuite) mockCryptsetupForAddKey(c *C) *testutil.MockCmd {
	cmd := testutil.MockCommand(c, "cryptsetup", fmt.Sprintf(`
while [ "$#" -gt 1 ]; do
  case "$1" in
    luksAddKey)
      cat > %s
      shift
      ;;
    --key-file)
      cat "$2" > %s
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done
`, filepath.Join(s.rootDir, "new.key"), filepath.Join(s.rootDir, "unlock.key")))
	return cmd
}

func (s *keymgrSuite) verifyCryptsetupAddKey(c *C, cmd *testutil.MockCmd, unlockKey, newKey []byte) {
	c.Assert(cmd, NotNil)
	calls := cmd.Calls()
	c.Assert(calls, HasLen, 2)
	c.Assert(calls[0], HasLen, 16)
	c.Assert(calls[0][5], testutil.Contains, s.rootDir)
	calls[0][5] = "<fifo>"
	c.Assert(calls[0], DeepEquals, []string{
		"cryptsetup", "luksAddKey", "--type", "luks2",
		"--key-file", "<fifo>",
		"--pbkdf", "argon2i",
		"--pbkdf-force-iterations", "4",
		"--pbkdf-memory", "202834",
		"--key-slot", "1",
		"/dev/foobar", "-",
	})
	c.Assert(calls[1], DeepEquals, []string{
		"cryptsetup", "config", "--priority", "prefer", "--key-slot", "0", "/dev/foobar",
	})
	c.Check(filepath.Join(s.rootDir, "unlock.key"), testutil.FileEquals, unlockKey)
	c.Check(filepath.Join(s.rootDir, "new.key"), testutil.FileEquals, newKey)
}

func (s *keymgrSuite) TestAddRecoveryKeyToDeviceUnlockFromKeyring(c *C) {
	unlockKey := "1234abcd"
	getCalls := 0
	restore := keymgr.MockGetDiskUnlockKeyFromKernel(func(prefix, devicePath string, remove bool) (sb.DiskUnlockKey, error) {
		getCalls++
		c.Check(devicePath, Equals, "/dev/foobar")
		c.Check(remove, Equals, false)
		c.Check(prefix, Equals, "ubuntu-fde")
		return []byte(unlockKey), nil
	})
	defer restore()

	cmd := s.mockCryptsetupForAddKey(c)
	defer cmd.Restore()
	err := keymgr.AddRecoveryKeyToLUKSDevice(mockRecoveryKey, "/dev/foobar")
	c.Assert(err, IsNil)
	c.Assert(getCalls, Equals, 1)
	s.verifyCryptsetupAddKey(c, cmd, []byte(unlockKey), mockRecoveryKey[:])
}

func (s *keymgrSuite) TestAddRecoveryKeyToDeviceNoUnlockKey(c *C) {
	getCalls := 0
	restore := keymgr.MockGetDiskUnlockKeyFromKernel(func(prefix, devicePath string, remove bool) (sb.DiskUnlockKey, error) {
		getCalls++
		c.Check(devicePath, Equals, "/dev/foobar")
		return nil, fmt.Errorf("cannot find key in kernel keyring")
	})
	defer restore()

	cmd := s.mockCryptsetupForAddKey(c)
	defer cmd.Restore()
	err := keymgr.AddRecoveryKeyToLUKSDevice(mockRecoveryKey, "/dev/foobar")
	c.Assert(err, ErrorMatches, "cannot obtain current unlock key for /dev/foobar: cannot find key in kernel keyring")
	c.Assert(getCalls, Equals, 1)
	c.Assert(cmd.Calls(), HasLen, 0)
}

func (s *keymgrSuite) TestAddRecoveryKeyToDeviceCryptsetupFail(c *C) {
	unlockKey := "1234abcd"
	restore := keymgr.MockGetDiskUnlockKeyFromKernel(func(prefix, devicePath string, remove bool) (sb.DiskUnlockKey, error) {
		return []byte(unlockKey), nil
	})
	defer restore()

	cmd := testutil.MockCommand(c, "cryptsetup", `
while [ "$#" -gt 1 ]; do
  case "$1" in
    --key-file)
      cat "$2" > /dev/null
      shift 2
      ;;
    *)
      shift 1
      ;;
  esac
done
echo "Other error, cryptsetup boom"
exit 1
`)
	defer cmd.Restore()
	err := keymgr.AddRecoveryKeyToLUKSDevice(mockRecoveryKey, "/dev/foobar")
	c.Assert(err, ErrorMatches, "cannot add key: cryptsetup failed with: Other error, cryptsetup boom")
	// should match the keyslot full error too
	c.Assert(keymgr.IsKeyslotAlreadyUsed(err), Equals, false)
}

func (s *keymgrSuite) TestAddRecoveryKeyToDeviceOccupiedSlot(c *C) {
	unlockKey := "1234abcd"
	getCalls := 0
	restore := keymgr.MockGetDiskUnlockKeyFromKernel(func(prefix, devicePath string, remove bool) (sb.DiskUnlockKey, error) {
		getCalls++
		c.Check(devicePath, Equals, "/dev/foobar")
		c.Check(remove, Equals, false)
		c.Check(prefix, Equals, "ubuntu-fde")
		return []byte(unlockKey), nil
	})
	defer restore()

	cmd := testutil.MockCommand(c, "cryptsetup", `
while [ "$#" -gt 1 ]; do
  case "$1" in
    --key-file)
      cat "$2" > /dev/null
      shift 2
      ;;
    *)
      shift 1
      ;;
  esac
done
echo "Key slot 1 is full, please select another one." >&2
exit 1
`)
	defer cmd.Restore()
	err := keymgr.AddRecoveryKeyToLUKSDevice(mockRecoveryKey, "/dev/foobar")
	c.Assert(err, ErrorMatches, "cannot add key: cryptsetup failed with: Key slot 1 is full.*")
	c.Assert(getCalls, Equals, 1)
	calls := cmd.Calls()
	c.Assert(calls, HasLen, 1)
	c.Assert(calls[0], HasLen, 16)
	c.Assert(calls[0][:2], DeepEquals, []string{"cryptsetup", "luksAddKey"})
	// should match the keyslot full error too
	c.Assert(keymgr.IsKeyslotAlreadyUsed(err), Equals, true)
}

func (s *keymgrSuite) TestAddRecoveryKeyToDeviceUsingExistingKey(c *C) {
	restore := keymgr.MockGetDiskUnlockKeyFromKernel(func(prefix, devicePath string, remove bool) (sb.DiskUnlockKey, error) {
		return nil, fmt.Errorf("unexpected call")
	})
	defer restore()

	cmd := s.mockCryptsetupForAddKey(c)
	defer cmd.Restore()
	key := bytes.Repeat([]byte{1}, 32)
	err := keymgr.AddRecoveryKeyToLUKSDeviceUsingKey(mockRecoveryKey, keys.EncryptionKey(key), "/dev/foobar")
	c.Assert(err, IsNil)
	s.verifyCryptsetupAddKey(c, cmd, []byte(key), mockRecoveryKey[:])
}

func (s *keymgrSuite) TestRemoveRecoveryKeyFromDevice(c *C) {
	unlockKey := "1234abcd"
	getCalls := 0
	restore := keymgr.MockGetDiskUnlockKeyFromKernel(func(prefix, devicePath string, remove bool) (sb.DiskUnlockKey, error) {
		getCalls++
		c.Check(devicePath, Equals, "/dev/foobar")
		c.Check(remove, Equals, false)
		c.Check(prefix, Equals, "ubuntu-fde")
		return []byte(unlockKey), nil
	})
	defer restore()

	err := keymgr.RemoveRecoveryKeyFromLUKSDevice("/dev/foobar")
	c.Assert(err, IsNil)
	c.Assert(getCalls, Equals, 1)
	calls := s.cryptsetupCmd.Calls()
	c.Assert(calls, DeepEquals, [][]string{
		{"cryptsetup", "luksKillSlot", "--type", "luks2", "--key-file", "-", "/dev/foobar", "1"},
	})
}

func (s *keymgrSuite) TestRemoveRecoveryKeyFromDeviceAlreadyEmpty(c *C) {
	unlockKey := "1234abcd"
	getCalls := 0
	restore := keymgr.MockGetDiskUnlockKeyFromKernel(func(prefix, devicePath string, remove bool) (sb.DiskUnlockKey, error) {
		getCalls++
		return []byte(unlockKey), nil
	})
	defer restore()

	cmd := testutil.MockCommand(c, "cryptsetup", `
echo "Keyslot 1 is not active." >&2
exit 1
`)
	defer cmd.Restore()

	err := keymgr.RemoveRecoveryKeyFromLUKSDevice("/dev/foobar")
	c.Assert(err, IsNil)
	c.Assert(getCalls, Equals, 1)
	calls := cmd.Calls()
	c.Assert(calls, DeepEquals, [][]string{
		{"cryptsetup", "luksKillSlot", "--type", "luks2", "--key-file", "-", "/dev/foobar", "1"},
	})
}

func (s *keymgrSuite) TestRemoveRecoveryKeyFromDeviceKeyNotInKeyring(c *C) {
	getCalls := 0
	restore := keymgr.MockGetDiskUnlockKeyFromKernel(func(prefix, devicePath string, remove bool) (sb.DiskUnlockKey, error) {
		getCalls++
		c.Check(devicePath, Equals, "/dev/foobar")
		return nil, fmt.Errorf("cannot find key in kernel keyring")
	})
	defer restore()

	err := keymgr.RemoveRecoveryKeyFromLUKSDevice("/dev/foobar")
	c.Assert(err, ErrorMatches, "cannot obtain current unlock key for /dev/foobar: cannot find key in kernel keyring")
	c.Assert(getCalls, Equals, 1)
	c.Assert(s.cryptsetupCmd.Calls(), HasLen, 0)
}

func (s *keymgrSuite) TestRemoveRecoveryKeyFromDeviceUsingKey(c *C) {
	restore := keymgr.MockGetDiskUnlockKeyFromKernel(func(prefix, devicePath string, remove bool) (sb.DiskUnlockKey, error) {
		c.Fail()
		return nil, fmt.Errorf("unexpected call")
	})
	defer restore()

	cryptsetupCmd := testutil.MockCommand(c, "cryptsetup", fmt.Sprintf(`
while [ "$#" -gt 1 ]; do
  case "$1" in
    luksKillSlot)
      cat > %s
      shift
      ;;
    *)
      shift
      ;;
  esac
done
`, filepath.Join(s.rootDir, "unlock.key")))
	defer cryptsetupCmd.Restore()

	key := bytes.Repeat([]byte{1}, 32)
	err := keymgr.RemoveRecoveryKeyFromLUKSDeviceUsingKey(keys.EncryptionKey(key), "/dev/foobar")
	c.Assert(err, IsNil)
	calls := cryptsetupCmd.Calls()
	c.Assert(calls, DeepEquals, [][]string{
		{"cryptsetup", "luksKillSlot", "--type", "luks2", "--key-file", "-", "/dev/foobar", "1"},
	})
	c.Assert(filepath.Join(s.rootDir, "unlock.key"), testutil.FileEquals, key)
}

func (s *keymgrSuite) TestChangeEncryptionKeyHappy(c *C) {
	unlockKey := "1234abcd"
	getCalls := 0
	restore := keymgr.MockGetDiskUnlockKeyFromKernel(func(prefix, devicePath string, remove bool) (sb.DiskUnlockKey, error) {
		getCalls++
		c.Check(devicePath, Equals, "/dev/foobar")
		c.Check(remove, Equals, false)
		c.Check(prefix, Equals, "ubuntu-fde")
		return []byte(unlockKey), nil
	})
	defer restore()

	addCalls := 0
	restore = keymgr.MockAddKeyToUserKeyring(func(key []byte, devicePath, purpose, prefix string) error {
		addCalls++
		c.Assert(key, DeepEquals, bytes.Repeat([]byte{1}, 32))
		c.Check(devicePath, Equals, "/dev/foobar")
		c.Check(purpose, Equals, "unlock")
		c.Check(prefix, Equals, "ubuntu-fde")
		return nil
	})
	defer restore()

	cmd := testutil.MockCommand(c, "cryptsetup", `
while [ "$#" -gt 1 ]; do
  case "$1" in
    --key-file)
      cat "$2"
      shift 2
      ;;
    *)
      shift 1
      ;;
  esac
done
`)
	defer cmd.Restore()
	// try a too short key
	key := bytes.Repeat([]byte{1}, 12)
	err := keymgr.ChangeLUKSDeviceEncryptionKey(key, "/dev/foobar")
	c.Assert(err, ErrorMatches, "cannot use a key of size different than 32")

	key = bytes.Repeat([]byte{1}, 32)
	err = keymgr.ChangeLUKSDeviceEncryptionKey(key, "/dev/foobar")
	c.Assert(err, IsNil)
	c.Assert(getCalls, Equals, 1)
	c.Assert(addCalls, Equals, 1)
	calls := cmd.Calls()
	c.Assert(calls, HasLen, 6)
	c.Assert(calls[0], DeepEquals, []string{
		"cryptsetup", "luksKillSlot", "--type", "luks2", "--key-file", "-", "/dev/foobar", "2",
	})
	c.Assert(calls[1], HasLen, 14)
	c.Assert(calls[1][5], testutil.Contains, dirs.RunDir)
	calls[1][5] = "<fifo>"
	// temporary key
	c.Assert(calls[1], DeepEquals, []string{
		"cryptsetup", "luksAddKey", "--type", "luks2",
		"--key-file", "<fifo>",
		"--pbkdf", "argon2i",
		"--iter-time", "100",
		"--key-slot", "2",
		"/dev/foobar", "-",
	})
	c.Assert(calls[2], DeepEquals, []string{
		"cryptsetup", "luksKillSlot", "--type", "luks2", "--key-file", "-", "/dev/foobar", "0",
	})
	c.Assert(calls[3], HasLen, 14)
	c.Assert(calls[3][5], testutil.Contains, dirs.RunDir)
	calls[3][5] = "<fifo>"
	// actual new key
	c.Assert(calls[3], DeepEquals, []string{
		"cryptsetup", "luksAddKey", "--type", "luks2",
		"--key-file", "<fifo>",
		"--pbkdf", "argon2i",
		"--iter-time", "100",
		"--key-slot", "0",
		"/dev/foobar", "-",
	})
	// kill the temp key
	c.Assert(calls[4], DeepEquals, []string{
		"cryptsetup", "luksKillSlot", "--type", "luks2", "--key-file", "-", "/dev/foobar", "2",
	})
	// set priority
	c.Assert(calls[5], DeepEquals, []string{
		"cryptsetup", "config", "--priority", "prefer", "--key-slot", "0", "/dev/foobar",
	})
}

func (s *keymgrSuite) TestChangeEncryptionKeyGetUnlockFail(c *C) {
	getCalls := 0
	restore := keymgr.MockGetDiskUnlockKeyFromKernel(func(prefix, devicePath string, remove bool) (sb.DiskUnlockKey, error) {
		getCalls++
		c.Check(devicePath, Equals, "/dev/foobar")
		return nil, fmt.Errorf("cannot find key in kernel keyring")
	})
	defer restore()

	restore = keymgr.MockAddKeyToUserKeyring(func(key []byte, devicePath, purpose, prefix string) error {
		c.Fatalf("unexpected call")
		return fmt.Errorf("unexpected call")
	})
	defer restore()

	key := bytes.Repeat([]byte{1}, 32)
	err := keymgr.ChangeLUKSDeviceEncryptionKey(key, "/dev/foobar")
	c.Assert(err, ErrorMatches, "cannot obtain current unlock key for /dev/foobar: cannot find key in kernel keyring")
	c.Assert(s.cryptsetupCmd.Calls(), HasLen, 0)
}

func (s *keymgrSuite) TestChangeEncryptionKeyAddKeyringFails(c *C) {
	unlockKey := "1234abcd"
	getCalls := 0
	restore := keymgr.MockGetDiskUnlockKeyFromKernel(func(prefix, devicePath string, remove bool) (sb.DiskUnlockKey, error) {
		getCalls++
		c.Check(devicePath, Equals, "/dev/foobar")
		c.Check(remove, Equals, false)
		c.Check(prefix, Equals, "ubuntu-fde")
		return []byte(unlockKey), nil
	})
	defer restore()

	addCalls := 0
	restore = keymgr.MockAddKeyToUserKeyring(func(key []byte, devicePath, purpose, prefix string) error {
		addCalls++
		c.Assert(key, DeepEquals, bytes.Repeat([]byte{1}, 32))
		c.Check(devicePath, Equals, "/dev/foobar")
		c.Check(purpose, Equals, "unlock")
		c.Check(prefix, Equals, "ubuntu-fde")
		return fmt.Errorf("add keyring fails")
	})
	defer restore()

	cmd := testutil.MockCommand(c, "cryptsetup", `
while [ "$#" -gt 1 ]; do
  case "$1" in
    --key-file)
      cat "$2"
      shift 2
      ;;
    *)
      shift 1
      ;;
  esac
done
`)
	defer cmd.Restore()

	key := bytes.Repeat([]byte{1}, 32)
	err := keymgr.ChangeLUKSDeviceEncryptionKey(key, "/dev/foobar")
	c.Assert(err, ErrorMatches, "cannot add key to user keyring: add keyring fails")
	c.Assert(getCalls, Equals, 1)
	c.Assert(addCalls, Equals, 1)
	calls := cmd.Calls()
	c.Assert(calls, HasLen, 6)
}

func (s *keymgrSuite) TestChangeEncryptionTempKeyFails(c *C) {
	unlockKey := "1234abcd"
	getCalls := 0
	restore := keymgr.MockGetDiskUnlockKeyFromKernel(func(prefix, devicePath string, remove bool) (sb.DiskUnlockKey, error) {
		getCalls++
		return []byte(unlockKey), nil
	})
	defer restore()
	restore = keymgr.MockAddKeyToUserKeyring(func(key []byte, devicePath, purpose, prefix string) error {
		c.Fatalf("unexpected call")
		return fmt.Errorf("add keyring fails")
	})
	defer restore()

	cmd := testutil.MockCommand(c, "cryptsetup", `
while [ "$#" -gt 1 ]; do
  case "$1" in
    --key-file)
      cat "$2" > /dev/null
      shift
      ;;
    luksAddKey)
      add=1
      ;;
    --key-slot)
      if [ "$add" = "1" ] && [ "$2" = "2" ]; then
          echo "mock failure" >&2
          exit 1
      fi
      ;;
    *)
      ;;
  esac
  shift
done
`)
	defer cmd.Restore()

	key := bytes.Repeat([]byte{1}, 32)
	err := keymgr.ChangeLUKSDeviceEncryptionKey(key, "/dev/foobar")
	c.Assert(err, ErrorMatches, "cannot add temporary key: cryptsetup failed with: mock failure")
	c.Assert(getCalls, Equals, 1)
	calls := cmd.Calls()
	c.Assert(calls, HasLen, 2)
}

func (s *keymgrSuite) TestRecoveryKDF(c *C) {
	mockedMeminfoFile := filepath.Join(c.MkDir(), "meminfo")
	s.AddCleanup(osutil.MockProcMeminfo(mockedMeminfoFile))

	_, err := keymgr.RecoveryKDF()
	c.Assert(err, ErrorMatches, "cannot get usable memory for KDF parameters when adding the recovery key: open .*")

	c.Assert(ioutil.WriteFile(mockedMeminfoFile, []byte(mockedMeminfo), 0644), IsNil)

	opts, err := keymgr.RecoveryKDF()
	c.Assert(err, IsNil)
	c.Assert(opts, DeepEquals, &luks2.KDFOptions{
		MemoryKiB:       202834,
		ForceIterations: 4,
	})

	const lotsOfMem = `MemTotal:         2097152 kB
CmaTotal:         131072 kB
`
	c.Assert(ioutil.WriteFile(mockedMeminfoFile, []byte(lotsOfMem), 0644), IsNil)
	opts, err = keymgr.RecoveryKDF()
	c.Assert(err, IsNil)
	c.Assert(opts, DeepEquals, &luks2.KDFOptions{
		MemoryKiB:       786432,
		ForceIterations: 4,
	})

	const littleMem = `MemTotal:         262144 kB
CmaTotal:         131072 kB
`
	c.Assert(ioutil.WriteFile(mockedMeminfoFile, []byte(littleMem), 0644), IsNil)
	opts, err = keymgr.RecoveryKDF()
	c.Assert(err, IsNil)
	c.Assert(opts, DeepEquals, &luks2.KDFOptions{
		MemoryKiB:       32,
		ForceIterations: 4,
	})
}
