// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package fde_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/kernel/fde"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

func TestFde(t *testing.T) { TestingT(t) }

type fdeSuite struct {
	testutil.BaseTest
}

var _ = Suite(&fdeSuite{})

func (s *fdeSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })
}

func (s *fdeSuite) TestHasRevealKey(c *C) {
	oldPath := os.Getenv("PATH")
	defer func() { os.Setenv("PATH", oldPath) }()

	mockRoot := c.MkDir()
	os.Setenv("PATH", mockRoot+"/bin")
	mockBin := mockRoot + "/bin/"
	err := os.Mkdir(mockBin, 0755)
	c.Assert(err, IsNil)

	// no fde-reveal-key binary
	c.Check(fde.HasRevealKey(), Equals, false)

	// fde-reveal-key without +x
	err = ioutil.WriteFile(mockBin+"fde-reveal-key", nil, 0644)
	c.Assert(err, IsNil)
	c.Check(fde.HasRevealKey(), Equals, false)

	// correct fde-reveal-key, no logging
	err = os.Chmod(mockBin+"fde-reveal-key", 0755)
	c.Assert(err, IsNil)

	c.Check(fde.HasRevealKey(), Equals, true)
}

func (s *fdeSuite) TestInitialSetupV2(c *C) {
	mockKey := []byte{1, 2, 3, 4}

	runSetupHook := func(req *fde.SetupRequest) ([]byte, error) {
		c.Check(req, DeepEquals, &fde.SetupRequest{
			Op:      "initial-setup",
			Key:     mockKey,
			KeyName: "some-key-name",
		})
		// sealed-key/handle
		mockJSON := fmt.Sprintf(`{"sealed-key":"%s", "handle":{"some":"handle"}}`, base64.StdEncoding.EncodeToString([]byte("the-encrypted-key")))
		return []byte(mockJSON), nil
	}

	params := &fde.InitialSetupParams{
		Key:     mockKey,
		KeyName: "some-key-name",
	}
	res, err := fde.InitialSetup(runSetupHook, params)
	c.Assert(err, IsNil)
	expectedHandle := json.RawMessage([]byte(`{"some":"handle"}`))
	c.Check(res, DeepEquals, &fde.InitialSetupResult{
		EncryptedKey: []byte("the-encrypted-key"),
		Handle:       &expectedHandle,
	})
}

func (s *fdeSuite) TestInitialSetupError(c *C) {
	mockKey := []byte{1, 2, 3, 4}

	errHook := errors.New("hook running error")
	runSetupHook := func(req *fde.SetupRequest) ([]byte, error) {
		c.Check(req, DeepEquals, &fde.SetupRequest{
			Op:      "initial-setup",
			Key:     mockKey,
			KeyName: "some-key-name",
		})
		return nil, errHook
	}

	params := &fde.InitialSetupParams{
		Key:     mockKey,
		KeyName: "some-key-name",
	}
	_, err := fde.InitialSetup(runSetupHook, params)
	c.Check(err, Equals, errHook)
}

func (s *fdeSuite) TestInitialSetupV1(c *C) {
	mockKey := []byte{1, 2, 3, 4}

	runSetupHook := func(req *fde.SetupRequest) ([]byte, error) {
		c.Check(req, DeepEquals, &fde.SetupRequest{
			Op:      "initial-setup",
			Key:     mockKey,
			KeyName: "some-key-name",
		})
		// needs the USK$ prefix to simulate v1 key
		return []byte("USK$sealed-key"), nil
	}

	params := &fde.InitialSetupParams{
		Key:     mockKey,
		KeyName: "some-key-name",
	}
	res, err := fde.InitialSetup(runSetupHook, params)
	c.Assert(err, IsNil)
	expectedHandle := json.RawMessage(`{"v1-no-handle":true}`)
	c.Assert(json.Valid(expectedHandle), Equals, true)
	c.Check(res, DeepEquals, &fde.InitialSetupResult{
		EncryptedKey: []byte("USK$sealed-key"),
		Handle:       &expectedHandle,
	})
}

func (s *fdeSuite) TestInitialSetupBadJSON(c *C) {
	mockKey := []byte{1, 2, 3, 4}

	runSetupHook := func(req *fde.SetupRequest) ([]byte, error) {
		return []byte("bad json"), nil
	}

	params := &fde.InitialSetupParams{
		Key:     mockKey,
		KeyName: "some-key-name",
	}
	_, err := fde.InitialSetup(runSetupHook, params)
	c.Check(err, ErrorMatches, `cannot decode hook output "bad json": invalid char.*`)
}

func checkSystemdRunOrSkip(c *C) {
	// this test uses a real systemd-run --user so check here if that
	// actually works
	if output, err := exec.Command("systemd-run", "--user", "--wait", "--collect", "--service-type=exec", "/bin/true").CombinedOutput(); err != nil {
		c.Skip(fmt.Sprintf("systemd-run not working: %v", osutil.OutputErr(output, err)))
	}

}

func (s *fdeSuite) TestLockSealedKeysCallsFdeReveal(c *C) {
	checkSystemdRunOrSkip(c)

	restore := fde.MockFdeInitramfsHelperCommandExtra([]string{"--user"})
	defer restore()
	fdeRevealKeyStdin := filepath.Join(c.MkDir(), "stdin")
	mockSystemdRun := testutil.MockCommand(c, "fde-reveal-key", fmt.Sprintf(`
cat - > %s
`, fdeRevealKeyStdin))
	defer mockSystemdRun.Restore()

	err := fde.LockSealedKeys()
	c.Assert(err, IsNil)
	c.Check(mockSystemdRun.Calls(), DeepEquals, [][]string{
		{"fde-reveal-key"},
	})
	c.Check(fdeRevealKeyStdin, testutil.FileEquals, `{"op":"lock"}`)

	// ensure no tmp files are left behind
	c.Check(osutil.FileExists(filepath.Join(dirs.GlobalRootDir, "/run/fde-reveal-key")), Equals, false)
}

func (s *fdeSuite) TestLockSealedKeysHonorsRuntimeMax(c *C) {
	checkSystemdRunOrSkip(c)

	restore := fde.MockFdeInitramfsHelperCommandExtra([]string{"--user"})
	defer restore()
	mockSystemdRun := testutil.MockCommand(c, "fde-reveal-key", "sleep 60")
	defer mockSystemdRun.Restore()

	restore = fde.MockFdeRevealKeyPollWaitParanoiaFactor(100)
	defer restore()

	restore = fde.MockFdeRevealKeyRuntimeMax(100 * time.Millisecond)
	defer restore()

	err := fde.LockSealedKeys()
	c.Assert(err, ErrorMatches, `cannot run fde-reveal-key "lock": service result: timeout`)
}

func (s *fdeSuite) TestLockSealedKeysHonorsParanoia(c *C) {
	checkSystemdRunOrSkip(c)

	restore := fde.MockFdeInitramfsHelperCommandExtra([]string{"--user"})
	defer restore()
	mockSystemdRun := testutil.MockCommand(c, "fde-reveal-key", "sleep 60")
	defer mockSystemdRun.Restore()

	restore = fde.MockFdeRevealKeyPollWaitParanoiaFactor(1)
	defer restore()

	// shorter than the fdeRevealKeyPollWait time
	restore = fde.MockFdeRevealKeyRuntimeMax(1 * time.Millisecond)
	defer restore()

	err := fde.LockSealedKeys()
	c.Assert(err, ErrorMatches, `cannot run fde-reveal-key "lock": internal error: systemd-run did not honor RuntimeMax=1ms setting`)
}

func (s *fdeSuite) TestReveal(c *C) {
	checkSystemdRunOrSkip(c)

	// fix randutil outcome
	rand.Seed(1)

	sealedKey := []byte("sealed-v2-payload")
	v2payload := []byte("unsealed-v2-payload")

	restore := fde.MockFdeInitramfsHelperCommandExtra([]string{"--user"})
	defer restore()
	fdeRevealKeyStdin := filepath.Join(c.MkDir(), "stdin")
	mockSystemdRun := testutil.MockCommand(c, "fde-reveal-key", fmt.Sprintf(`
cat - > %s
printf '{"key": "%s"}'
`, fdeRevealKeyStdin, base64.StdEncoding.EncodeToString(v2payload)))
	defer mockSystemdRun.Restore()

	handle := json.RawMessage(`{"some": "handle"}`)
	p := fde.RevealParams{
		SealedKey: sealedKey,
		Handle:    &handle,
		V2Payload: true,
	}
	res, err := fde.Reveal(&p)
	c.Assert(err, IsNil)
	c.Check(res, DeepEquals, v2payload)
	c.Check(mockSystemdRun.Calls(), DeepEquals, [][]string{
		{"fde-reveal-key"},
	})
	c.Check(fdeRevealKeyStdin, testutil.FileEquals, fmt.Sprintf(`{"op":"reveal","sealed-key":%q,"handle":{"some":"handle"},"key-name":"deprecated-pw7MpXh0JB4P"}`, base64.StdEncoding.EncodeToString(sealedKey)))

	// ensure no tmp files are left behind
	c.Check(osutil.FileExists(filepath.Join(dirs.GlobalRootDir, "/run/fde-reveal-key")), Equals, false)
}

func (s *fdeSuite) TestRevealV1(c *C) {
	// this test that v1 hooks and raw binary v1 created sealedKey files still work
	checkSystemdRunOrSkip(c)

	// fix randutil outcome
	rand.Seed(1)

	restore := fde.MockFdeInitramfsHelperCommandExtra([]string{"--user"})
	defer restore()
	fdeRevealKeyStdin := filepath.Join(c.MkDir(), "stdin")
	mockSystemdRun := testutil.MockCommand(c, "fde-reveal-key", fmt.Sprintf(`
cat - > %s
printf "unsealed-key-64-chars-long-when-not-json-to-match-denver-project"
`, fdeRevealKeyStdin))
	defer mockSystemdRun.Restore()

	sealedKey := []byte("sealed-key")
	p := fde.RevealParams{
		SealedKey: sealedKey,
	}
	res, err := fde.Reveal(&p)
	c.Assert(err, IsNil)
	c.Check(res, DeepEquals, []byte("unsealed-key-64-chars-long-when-not-json-to-match-denver-project"))
	c.Check(mockSystemdRun.Calls(), DeepEquals, [][]string{
		{"fde-reveal-key"},
	})
	c.Check(fdeRevealKeyStdin, testutil.FileEquals, fmt.Sprintf(`{"op":"reveal","sealed-key":%q,"key-name":"deprecated-pw7MpXh0JB4P"}`, base64.StdEncoding.EncodeToString([]byte("sealed-key"))))

	// ensure no tmp files are left behind
	c.Check(osutil.FileExists(filepath.Join(dirs.GlobalRootDir, "/run/fde-reveal-key")), Equals, false)
}

func (s *fdeSuite) TestRevealV2PayloadV1Hook(c *C) {
	checkSystemdRunOrSkip(c)

	// fix randutil outcome
	rand.Seed(1)

	sealedKey := []byte("sealed-v2-payload")
	v2payload := []byte("unsealed-v2-payload")

	restore := fde.MockFdeInitramfsHelperCommandExtra([]string{"--user"})
	defer restore()
	fdeRevealKeyStdin := filepath.Join(c.MkDir(), "stdin")
	mockSystemdRun := testutil.MockCommand(c, "fde-reveal-key", fmt.Sprintf(`
cat - > %s
printf %q
`, fdeRevealKeyStdin, v2payload))
	defer mockSystemdRun.Restore()

	handle := json.RawMessage(`{"v1-no-handle":true}`)
	p := fde.RevealParams{
		SealedKey: sealedKey,
		Handle:    &handle,
		V2Payload: true,
	}
	res, err := fde.Reveal(&p)
	c.Assert(err, IsNil)
	c.Check(res, DeepEquals, v2payload)
	c.Check(mockSystemdRun.Calls(), DeepEquals, [][]string{
		{"fde-reveal-key"},
	})
	c.Check(fdeRevealKeyStdin, testutil.FileEquals, fmt.Sprintf(`{"op":"reveal","sealed-key":%q,"key-name":"deprecated-pw7MpXh0JB4P"}`, base64.StdEncoding.EncodeToString(sealedKey)))

	// ensure no tmp files are left behind
	c.Check(osutil.FileExists(filepath.Join(dirs.GlobalRootDir, "/run/fde-reveal-key")), Equals, false)
}

func (s *fdeSuite) TestRevealV2BadJSON(c *C) {
	// we need let higher level deal with this
	checkSystemdRunOrSkip(c)

	// fix randutil outcome
	rand.Seed(1)

	sealedKey := []byte("sealed-v2-payload")

	restore := fde.MockFdeInitramfsHelperCommandExtra([]string{"--user"})
	defer restore()
	fdeRevealKeyStdin := filepath.Join(c.MkDir(), "stdin")
	mockSystemdRun := testutil.MockCommand(c, "fde-reveal-key", fmt.Sprintf(`
cat - > %s
printf 'invalid-json'
`, fdeRevealKeyStdin))
	defer mockSystemdRun.Restore()

	handle := json.RawMessage(`{"some": "handle"}`)
	p := fde.RevealParams{
		SealedKey: sealedKey,
		Handle:    &handle,
		V2Payload: true,
	}
	res, err := fde.Reveal(&p)
	c.Assert(err, IsNil)
	// we just get the bad json out
	c.Check(res, DeepEquals, []byte("invalid-json"))
	c.Check(mockSystemdRun.Calls(), DeepEquals, [][]string{
		{"fde-reveal-key"},
	})
	c.Check(fdeRevealKeyStdin, testutil.FileEquals, fmt.Sprintf(`{"op":"reveal","sealed-key":%q,"handle":{"some":"handle"},"key-name":"deprecated-pw7MpXh0JB4P"}`, base64.StdEncoding.EncodeToString(sealedKey)))

	// ensure no tmp files are left behind
	c.Check(osutil.FileExists(filepath.Join(dirs.GlobalRootDir, "/run/fde-reveal-key")), Equals, false)
}

func (s *fdeSuite) TestRevealV1BadOutputSize(c *C) {
	checkSystemdRunOrSkip(c)

	// fix randutil outcome
	rand.Seed(1)

	restore := fde.MockFdeInitramfsHelperCommandExtra([]string{"--user"})
	defer restore()
	fdeRevealKeyStdin := filepath.Join(c.MkDir(), "stdin")
	mockSystemdRun := testutil.MockCommand(c, "fde-reveal-key", fmt.Sprintf(`
cat - > %s
printf "bad-size"
`, fdeRevealKeyStdin))
	defer mockSystemdRun.Restore()

	sealedKey := []byte("sealed-key")
	p := fde.RevealParams{
		SealedKey: sealedKey,
	}
	_, err := fde.Reveal(&p)
	c.Assert(err, ErrorMatches, `cannot decode fde-reveal-key \"reveal\" result: .*`)

	c.Check(osutil.FileExists(filepath.Join(dirs.GlobalRootDir, "/run/fde-reveal-key")), Equals, false)
}

func (s *fdeSuite) TestedRevealTruncatesStreamFiles(c *C) {
	checkSystemdRunOrSkip(c)

	// fix randutil outcome
	rand.Seed(1)

	// create the temporary output file streams with garbage data to ensure that
	// by the time the hook runs the files are emptied and recreated with the
	// right permissions
	streamFiles := []string{}
	for _, stream := range []string{"stdin", "stdout", "stderr"} {
		streamFile := filepath.Join(dirs.GlobalRootDir, "/run/fde-reveal-key/fde-reveal-key."+stream)
		streamFiles = append(streamFiles, streamFile)
		// make the dir 0700
		err := os.MkdirAll(filepath.Dir(streamFile), 0700)
		c.Assert(err, IsNil)
		// but make the file world-readable as it should be reset to 0600 before
		// the hook is run
		err = ioutil.WriteFile(streamFile, []byte("blah blah blah blah blah blah blah blah blah blah"), 0755)
		c.Assert(err, IsNil)
	}

	// the hook script only verifies that the stdout file is empty since we
	// need to write to the stderr file for performing the test, but we still
	// check the stderr file for correct permissions
	mockSystemdRun := testutil.MockCommand(c, "fde-reveal-key", fmt.Sprintf(`
# check that stdin has the right sealed key content
if [ "$(cat %[1]s)" != "{\"op\":\"reveal\",\"sealed-key\":\"AQIDBA==\",\"key-name\":\"deprecated-pw7MpXh0JB4P\"}" ]; then
	echo "test failed: stdin file has wrong content: $(cat %[1]s)" 1>&2
else
	echo "stdin file has correct content" 1>&2
fi

# check that stdout is empty
if [ -n "$(cat %[2]s)" ]; then
	echo "test failed: stdout file is not empty: $(cat %[2]s)" 1>&2
else
	echo "stdout file is correctly empty" 1>&2
fi

# check that stdin has the right 600 perms
if [ "$(stat --format=%%a %[1]s)" != "600" ]; then
	echo "test failed: stdin file has wrong permissions: $(stat --format=%%a %[1]s)" 1>&2
else
	echo "stdin file has correct 600 permissions" 1>&2
fi

# check that stdout has the right 600 perms
if [ "$(stat --format=%%a %[2]s)" != "600" ]; then
	echo "test failed: stdout file has wrong permissions: $(stat --format=%%a %[2]s)" 1>&2
else
	echo "stdout file has correct 600 permissions" 1>&2
fi

# check that stderr has the right 600 perms
if [ "$(stat --format=%%a %[3]s)" != "600" ]; then
	echo "test failed: stderr file has wrong permissions: $(stat --format=%%a %[3]s)" 1>&2
else
	echo "stderr file has correct 600 permissions" 1>&2
fi

echo "making the hook always fail for simpler test code" 1>&2

# always make the hook exit 1 for simpler test code
exit 1
`, streamFiles[0], streamFiles[1], streamFiles[2]))
	defer mockSystemdRun.Restore()
	restore := fde.MockFdeInitramfsHelperCommandExtra([]string{"--user"})
	defer restore()

	sealedKey := []byte{1, 2, 3, 4}
	p := fde.RevealParams{
		SealedKey: sealedKey,
	}
	_, err := fde.Reveal(&p)
	c.Assert(err, ErrorMatches, `(?s)cannot run fde-reveal-key "reveal": 
-----
stdin file has correct content
stdout file is correctly empty
stdin file has correct 600 permissions
stdout file has correct 600 permissions
stderr file has correct 600 permissions
making the hook always fail for simpler test code
service result: exit-code
-----`)
	// ensure no tmp files are left behind
	c.Check(osutil.FileExists(filepath.Join(dirs.GlobalRootDir, "/run/fde-reveal-key")), Equals, false)
}

func (s *fdeSuite) TestRevealErr(c *C) {
	checkSystemdRunOrSkip(c)

	// fix randutil outcome
	rand.Seed(1)

	mockSystemdRun := testutil.MockCommand(c, "systemd-run", `echo failed 1>&2; false`)
	defer mockSystemdRun.Restore()
	restore := fde.MockFdeInitramfsHelperCommandExtra([]string{"--user"})
	defer restore()

	sealedKey := []byte{1, 2, 3, 4}
	p := fde.RevealParams{
		SealedKey: sealedKey,
	}
	_, err := fde.Reveal(&p)
	c.Assert(err, ErrorMatches, `(?s)cannot run fde-reveal-key "reveal": failed`)

	root := dirs.GlobalRootDir
	calls := mockSystemdRun.Calls()
	c.Check(calls, DeepEquals, [][]string{
		{
			"systemd-run", "--collect", "--service-type=exec", "--quiet",
			"--property=RuntimeMaxSec=2m0s",
			"--property=SystemCallFilter=~@mount",
			fmt.Sprintf("--property=StandardInput=file:%s/run/fde-reveal-key/fde-reveal-key.stdin", root),
			fmt.Sprintf("--property=StandardOutput=file:%s/run/fde-reveal-key/fde-reveal-key.stdout", root),
			fmt.Sprintf("--property=StandardError=file:%s/run/fde-reveal-key/fde-reveal-key.stderr", root),
			fmt.Sprintf(`--property=ExecStopPost=/bin/sh -c 'if [ "$EXIT_STATUS" = 0 ]; then touch %[1]s/run/fde-reveal-key/fde-reveal-key.success; else echo "service result: $SERVICE_RESULT" >%[1]s/run/fde-reveal-key/fde-reveal-key.failed; fi'`, root),
			"--user",
			"fde-reveal-key",
		},
	})
	// ensure no tmp files are left behind
	c.Check(osutil.FileExists(filepath.Join(dirs.GlobalRootDir, "/run/fde-reveal-key")), Equals, false)
}

func (s *fdeSuite) TestDeviceSetupHappy(c *C) {
	mockKey := []byte{1, 2, 3, 4}
	mockDevice := "/dev/sda2"

	runSetupHook := func(req *fde.SetupRequest) ([]byte, error) {
		c.Check(req, DeepEquals, &fde.SetupRequest{
			Op:     "device-setup",
			Key:    mockKey,
			Device: mockDevice,
		})
		// empty reply: no error
		mockJSON := `{}`
		return []byte(mockJSON), nil
	}

	params := &fde.DeviceSetupParams{
		Key:    mockKey,
		Device: mockDevice,
	}
	err := fde.DeviceSetup(runSetupHook, params)
	c.Assert(err, IsNil)
}

func (s *fdeSuite) TestDeviceSetupError(c *C) {
	mockKey := []byte{1, 2, 3, 4}
	mockDevice := "/dev/sda2"

	runSetupHook := func(req *fde.SetupRequest) ([]byte, error) {
		c.Check(req, DeepEquals, &fde.SetupRequest{
			Op:     "device-setup",
			Key:    mockKey,
			Device: mockDevice,
		})
		// empty reply: no error
		mockJSON := `something failed badly`
		return []byte(mockJSON), fmt.Errorf("exit status 1")
	}

	params := &fde.DeviceSetupParams{
		Key:    mockKey,
		Device: mockDevice,
	}
	err := fde.DeviceSetup(runSetupHook, params)
	c.Check(err, ErrorMatches, "device setup failed with: something failed badly")
}

func (s *fdeSuite) TestDeviceUnlock(c *C) {
	checkSystemdRunOrSkip(c)

	restore := fde.MockFdeInitramfsHelperCommandExtra([]string{"--user"})
	defer restore()
	fdeDeviceUnlockStdin := filepath.Join(c.MkDir(), "stdin")
	mockDeviceUnlockHook := testutil.MockCommand(c, "fde-device-unlock", fmt.Sprintf(`
cat - > %s
`, fdeDeviceUnlockStdin))
	defer mockDeviceUnlockHook.Restore()

	// ensure we have a device-unlock hook
	c.Assert(fde.HasDeviceUnlock(), Equals, true)
	key := []byte{0, 1, 2, 3, 4, 5}
	err := fde.DeviceUnlock(&fde.DeviceUnlockParams{
		Key:           key,
		Device:        "/dev/mapper/data-device-locked",
		PartitionName: "data",
	})
	c.Assert(err, IsNil)
	c.Check(mockDeviceUnlockHook.Calls(), DeepEquals, [][]string{
		{"fde-device-unlock"},
	})
	exp := fmt.Sprintf(`{"op":"device-unlock","key":%q,"device":"/dev/mapper/data-device-locked","partition-name":"data"}`, base64.StdEncoding.EncodeToString(key))
	c.Check(fdeDeviceUnlockStdin, testutil.FileEquals, exp)

	// ensure no tmp files are left behind
	c.Check(osutil.FileExists(filepath.Join(dirs.GlobalRootDir, "/run/fde-device-unlock")), Equals, false)
}

func (s *fdeSuite) TestDeviceUnlockErr(c *C) {
	checkSystemdRunOrSkip(c)

	restore := fde.MockFdeInitramfsHelperCommandExtra([]string{"--user"})
	defer restore()
	mockDeviceUnlockHook := testutil.MockCommand(c, "fde-device-unlock", `
echo  "output-only-used-for-errors" 1>&2
exit 1
`)
	defer mockDeviceUnlockHook.Restore()

	p := fde.DeviceUnlockParams{
		Key:           []byte{0, 1, 2, 3, 4, 5},
		Device:        "/dev/mapper/data-device-locked",
		PartitionName: "data",
	}
	err := fde.DeviceUnlock(&p)
	c.Assert(err, ErrorMatches, `cannot run fde-device-unlock "device-unlock": 
-----
output-only-used-for-errors
service result: exit-code
-----`)

	c.Assert(mockDeviceUnlockHook.Calls(), DeepEquals, [][]string{
		{"fde-device-unlock"},
	})

	// ensure no tmp files are left behind
	c.Check(osutil.FileExists(filepath.Join(dirs.GlobalRootDir, "/run/fde-device-unlock")), Equals, false)
}

func (s *fdeSuite) TestHasDeviceUnlock(c *C) {
	oldPath := os.Getenv("PATH")
	defer func() { os.Setenv("PATH", oldPath) }()

	mockRoot := c.MkDir()
	os.Setenv("PATH", mockRoot+"/bin")
	mockBin := mockRoot + "/bin/"
	err := os.Mkdir(mockBin, 0755)
	c.Assert(err, IsNil)

	// no fde-device-unlock binary
	c.Check(fde.HasDeviceUnlock(), Equals, false)

	// fde-device-unlock without +x
	err = ioutil.WriteFile(mockBin+"fde-device-unlock", nil, 0644)
	c.Assert(err, IsNil)
	c.Check(fde.HasDeviceUnlock(), Equals, false)

	// correct fde-device-unlock, no logging
	err = os.Chmod(mockBin+"fde-device-unlock", 0755)
	c.Assert(err, IsNil)

	c.Check(fde.HasDeviceUnlock(), Equals, true)
}

func (s *fdeSuite) TestIsEncryptedDeviceMapperName(c *C) {
	// matches
	for _, t := range []string{
		"something-device-locked",
		"foo23-device-locked",
		"device-device-locked",
		"WE-DON'T-CARE-WHAT-THE-PREFIX-IS-AND-YOU-CAN'T-MAKE-US-device-locked",
	} {
		c.Assert(fde.IsHardwareEncryptedDeviceMapperName(t), Equals, true)
	}

	// doesn't match
	for _, t := range []string{
		"",
		"-device-locked",
		"device-locked",
		"-device-locked-foo",
		"some-device",
		"CRYPT-LUKS2-5a522809c87e4dfa81a88dc5667d1304-ubuntu-data-3776bab4-8bcc-46b7-9da2-6a84ce7f93b4",
	} {
		c.Assert(fde.IsHardwareEncryptedDeviceMapperName(t), Equals, false)
	}
}

func (s *fdeSuite) TestEncryptedDeviceMapperName(c *C) {
	for _, str := range []string{
		"ubuntu-data",
		"ubuntu-save",
		"foo",
		"other",
	} {
		c.Assert(fde.EncryptedDeviceMapperName(str), Equals, str+"-device-locked")
	}
}
