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
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

func TestFde(t *testing.T) { TestingT(t) }

type fdeSuite struct {
	testutil.BaseTest

	sysd systemd.Systemd
}

var _ = Suite(&fdeSuite{})

func (s *fdeSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })

	s.sysd = systemd.New(systemd.UserMode, nil)
	s.AddCleanup(systemd.MockNewSystemd(func(be systemd.Backend, roodDir string, mode systemd.InstanceMode, meter systemd.Reporter) systemd.Systemd {
		return s.sysd
	}))
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
	err = os.WriteFile(mockBin+"fde-reveal-key", nil, 0644)
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

	mockSystemdRun := testutil.MockCommand(c, "fde-reveal-key", "sleep 60")
	defer mockSystemdRun.Restore()

	restore := fde.MockFdeRevealKeyRuntimeMax(100 * time.Millisecond)
	defer restore()

	err := fde.LockSealedKeys()
	c.Assert(err, ErrorMatches, `cannot run \["fde-reveal-key"\]: exit status 1`)
}

func (s *fdeSuite) TestReveal(c *C) {
	checkSystemdRunOrSkip(c)

	// fix randutil outcome
	rand.Seed(1)

	sealedKey := []byte("sealed-v2-payload")
	v2payload := []byte("unsealed-v2-payload")

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

func (s *fdeSuite) TestRevealErr(c *C) {
	checkSystemdRunOrSkip(c)

	// fix randutil outcome
	rand.Seed(1)

	mockSystemdRun := testutil.MockCommand(c, "systemd-run", `echo failed 1>&2; false`)
	defer mockSystemdRun.Restore()

	sealedKey := []byte{1, 2, 3, 4}
	p := fde.RevealParams{
		SealedKey: sealedKey,
	}
	_, err := fde.Reveal(&p)
	c.Assert(err, ErrorMatches, `(?s)cannot run [[]"fde-reveal-key"[]]: .*failed.*`)

	//root := dirs.GlobalRootDir
	calls := mockSystemdRun.Calls()
	c.Check(calls, DeepEquals, [][]string{
		{
			"systemd-run", "--wait", "--pipe", "--collect",
			"--service-type=exec", "--quiet", "--user",
			"--property=DefaultDependencies=no",
			"--property=SystemCallFilter=~@mount",
			"--property=RuntimeMaxSec=2m0s",
			"--",
			"fde-reveal-key",
		},
	})
	// ensure no tmp files are left behind
	c.Check(osutil.FileExists(filepath.Join(dirs.GlobalRootDir, "/run/fde-reveal-key")), Equals, false)
}
