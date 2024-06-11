// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018-2024 Canonical Ltd
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

package interfaces_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/sandbox/seccomp"
	"github.com/snapcore/snapd/testutil"
)

type systemKeySuite struct {
	testutil.BaseTest

	tmp                    string
	apparmorFeatures       string
	buildID                string
	seccompCompilerVersion seccomp.VersionInfo
}

var _ = Suite(&systemKeySuite{})

func (s *systemKeySuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.tmp = c.MkDir()
	dirs.SetRootDir(s.tmp)
	err := os.MkdirAll(filepath.Dir(dirs.SnapSystemKeyFile), 0755)
	c.Assert(err, IsNil)
	err = os.MkdirAll(dirs.DistroLibExecDir, 0755)
	c.Assert(err, IsNil)
	err = os.Symlink("/proc/self/exe", filepath.Join(dirs.DistroLibExecDir, "snapd"))
	c.Assert(err, IsNil)

	s.apparmorFeatures = filepath.Join(s.tmp, "/sys/kernel/security/apparmor/features")
	s.buildID = "this-is-my-build-id"

	s.seccompCompilerVersion = seccomp.VersionInfo("123 2.3.3 abcdef123 -")
	testutil.MockCommand(c, filepath.Join(dirs.DistroLibExecDir, "snap-seccomp"), fmt.Sprintf(`
if [ "$1" = "version-info" ]; then echo "%s"; exit 0; fi
exit 1
`, s.seccompCompilerVersion))

	s.AddCleanup(seccomp.MockActions([]string{"allow", "errno", "kill", "log", "trace", "trap"}))
}

func (s *systemKeySuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)

	dirs.SetRootDir("/")
}

func (s *systemKeySuite) testInterfaceWriteSystemKey(c *C, remoteFSHome, overlayRoot bool) {
	var overlay string
	if overlayRoot {
		overlay = "overlay"
	}
	restore := interfaces.MockIsHomeUsingRemoteFS(func() (bool, error) { return remoteFSHome, nil })
	defer restore()

	restore = interfaces.MockReadBuildID(func(p string) (string, error) {
		c.Assert(p, Equals, filepath.Join(dirs.DistroLibExecDir, "snapd"))
		return s.buildID, nil
	})
	defer restore()

	restore = interfaces.MockIsRootWritableOverlay(func() (string, error) { return overlay, nil })
	defer restore()

	restore = cgroup.MockVersion(1, nil)
	defer restore()

	promptingSupported := features.AppArmorPrompting.IsSupported()
	promptingEnabled := true
	extraData := interfaces.SystemKeyExtraData{
		AppArmorPrompting: promptingEnabled,
	}

	err := interfaces.WriteSystemKey(extraData)
	c.Assert(err, IsNil)

	systemKey, err := os.ReadFile(dirs.SnapSystemKeyFile)
	c.Assert(err, IsNil)

	kernelFeatures, _ := apparmor.KernelFeatures()

	apparmorFeaturesStr, err := json.Marshal(kernelFeatures)
	c.Assert(err, IsNil)

	apparmorParserMtime, err := json.Marshal(apparmor.ParserMtime())
	c.Assert(err, IsNil)

	parserFeatures, _ := apparmor.ParserFeatures()
	apparmorParserFeaturesStr, err := json.Marshal(parserFeatures)
	c.Assert(err, IsNil)

	apparmorPromptingStr, err := json.Marshal(promptingSupported && promptingEnabled)
	c.Assert(err, IsNil)

	seccompActionsStr, err := json.Marshal(seccomp.Actions())
	c.Assert(err, IsNil)

	compiler, err := seccomp.NewCompiler(func(name string) (string, error) {
		return filepath.Join(dirs.DistroLibExecDir, "snap-seccomp"), nil
	})
	c.Assert(err, IsNil)
	seccompCompilerVersion, err := compiler.VersionInfo()
	c.Assert(err, IsNil)
	c.Assert(seccompCompilerVersion, Equals, s.seccompCompilerVersion)

	c.Check(string(systemKey), testutil.EqualsWrapped, fmt.Sprintf(`{"version":%d,"build-id":"%s","apparmor-features":%s,"apparmor-parser-mtime":%s,"apparmor-parser-features":%s,"apparmor-prompting":%s,"nfs-home":%v,"overlay-root":%q,"seccomp-features":%s,"seccomp-compiler-version":"%s","cgroup-version":"1"}`,
		interfaces.SystemKeyVersion,
		s.buildID,
		apparmorFeaturesStr,
		apparmorParserMtime,
		apparmorParserFeaturesStr,
		apparmorPromptingStr,
		remoteFSHome,
		overlay,
		seccompActionsStr,
		seccompCompilerVersion,
	))
}

func (s *systemKeySuite) TestInterfaceWriteSystemKeyNoRemoteFS(c *C) {
	s.testInterfaceWriteSystemKey(c, false, false)
}

func (s *systemKeySuite) TestInterfaceWriteSystemKeyWithRemoteFS(c *C) {
	s.testInterfaceWriteSystemKey(c, true, false)
}

func (s *systemKeySuite) TestInterfaceWriteSystemKeyWithOverlayRoot(c *C) {
	s.testInterfaceWriteSystemKey(c, false, true)
}

// bonus points to someone who actually runs this
func (s *systemKeySuite) TestInterfaceWriteSystemKeyWithRemoteFSWithOverlayRoot(c *C) {
	s.testInterfaceWriteSystemKey(c, true, true)
}

func (s *systemKeySuite) TestInterfaceWriteSystemKeyErrorOnBuildID(c *C) {
	restore := interfaces.MockIsHomeUsingRemoteFS(func() (bool, error) { return false, nil })
	defer restore()

	restore = interfaces.MockReadBuildID(func(p string) (string, error) {
		c.Assert(p, Equals, filepath.Join(dirs.DistroLibExecDir, "snapd"))
		return "", fmt.Errorf("no build ID for you")
	})
	defer restore()

	extraData := interfaces.SystemKeyExtraData{}

	err := interfaces.WriteSystemKey(extraData)
	c.Assert(err, ErrorMatches, "no build ID for you")
}

func (s *systemKeySuite) TestInterfaceSystemKeyMismatchHappy(c *C) {
	s.AddCleanup(interfaces.MockSystemKey(`
{
"build-id": "7a94e9736c091b3984bd63f5aebfc883c4d859e0",
"apparmor-features": ["caps", "dbus"]
}
`))

	extraData := interfaces.SystemKeyExtraData{
		AppArmorPrompting: true,
	}

	// no system-key yet -> Error
	c.Assert(osutil.FileExists(dirs.SnapSystemKeyFile), Equals, false)
	_, err := interfaces.SystemKeyMismatch(extraData)
	c.Assert(err, Equals, interfaces.ErrSystemKeyMissing)

	// create a system-key -> no mismatch anymore
	err = interfaces.WriteSystemKey(extraData)
	c.Assert(err, IsNil)
	mismatch, err := interfaces.SystemKeyMismatch(extraData)
	c.Assert(err, IsNil)
	c.Check(mismatch, Equals, false)

	// change our system-key to have more apparmor features
	s.AddCleanup(interfaces.MockSystemKey(`
{
"build-id": "7a94e9736c091b3984bd63f5aebfc883c4d859e0",
"apparmor-features": ["caps", "dbus", "more", "and", "more"]
}
`))
	mismatch, err = interfaces.SystemKeyMismatch(extraData)
	c.Assert(err, IsNil)
	c.Check(mismatch, Equals, true)
}

func (s *systemKeySuite) TestInterfaceSystemKeyMismatchParserMtimeHappy(c *C) {
	s.AddCleanup(interfaces.MockSystemKey(`
{
"build-id": "7a94e9736c091b3984bd63f5aebfc883c4d859e0",
"apparmor-parser-mtime": 1234
}
`))

	extraData := interfaces.SystemKeyExtraData{}

	// no system-key yet -> Error
	c.Assert(osutil.FileExists(dirs.SnapSystemKeyFile), Equals, false)
	_, err := interfaces.SystemKeyMismatch(extraData)
	c.Assert(err, Equals, interfaces.ErrSystemKeyMissing)

	// create a system-key -> no mismatch anymore
	err = interfaces.WriteSystemKey(extraData)
	c.Assert(err, IsNil)
	mismatch, err := interfaces.SystemKeyMismatch(extraData)
	c.Assert(err, IsNil)
	c.Check(mismatch, Equals, false)

	// change our system-key to have a different parser mtime
	s.AddCleanup(interfaces.MockSystemKey(`
{
"build-id": "7a94e9736c091b3984bd63f5aebfc883c4d859e0",
"apparmor-parser-mtime": 5678
}
`))
	mismatch, err = interfaces.SystemKeyMismatch(extraData)
	c.Assert(err, IsNil)
	c.Check(mismatch, Equals, true)
}

func (s *systemKeySuite) TestInterfaceSystemKeyMismatchAppArmorPromptingHappy(c *C) {
	s.AddCleanup(interfaces.MockSystemKey(`
{
"build-id": "7a94e9736c091b3984bd63f5aebfc883c4d859e0",
"apparmor-prompting": false
}
`))

	extraData := interfaces.SystemKeyExtraData{
		AppArmorPrompting: true,
	}

	// no system-key yet -> Error
	c.Assert(osutil.FileExists(dirs.SnapSystemKeyFile), Equals, false)
	_, err := interfaces.SystemKeyMismatch(extraData)
	c.Assert(err, Equals, interfaces.ErrSystemKeyMissing)

	// create a system-key -> no mismatch anymore
	err = interfaces.WriteSystemKey(extraData)
	c.Assert(err, IsNil)
	// Even though prompting flag is enabled, since prompting unsupported,
	// both prompting-related fields will still be false.
	mismatch, err := interfaces.SystemKeyMismatch(extraData)
	c.Assert(err, IsNil)
	c.Check(mismatch, Equals, false)

	for _, testCase := range []struct {
		supported bool // previously (and currently) supported
		prevValue bool // previously supported and enabled
		newValue  bool // new "enabled" value
		mismatch  bool // whether there should be a mismatch
	}{
		{
			supported: false,
			prevValue: false,
			newValue:  false,
			mismatch:  false,
		},
		{
			supported: false,
			prevValue: false,
			newValue:  true,
			mismatch:  false,
		},
		{
			supported: true,
			prevValue: false,
			newValue:  false,
			mismatch:  false,
		},
		{
			supported: true,
			prevValue: true,
			newValue:  false,
			mismatch:  true,
		},
		{
			supported: true,
			prevValue: false,
			newValue:  true,
			mismatch:  true,
		},
		{
			supported: true,
			prevValue: true,
			newValue:  true,
			mismatch:  false,
		},
	} {
		s.AddCleanup(interfaces.MockSystemKey(fmt.Sprintf(`
{
"build-id": "7a94e9736c091b3984bd63f5aebfc883c4d859e0",
"apparmor-prompting": %t
}
`, testCase.prevValue)))

		restore := interfaces.MockApparmorPromptingSupportedByFeatures(func(apparmorFeatures *apparmor.FeaturesSupported) (bool, string) {
			return testCase.supported, ""
		})

		extraData = interfaces.SystemKeyExtraData{
			AppArmorPrompting: testCase.prevValue,
		}
		err = interfaces.WriteSystemKey(extraData)
		c.Assert(err, IsNil)

		extraData = interfaces.SystemKeyExtraData{
			AppArmorPrompting: testCase.newValue,
		}
		mismatch, err = interfaces.SystemKeyMismatch(extraData)
		c.Assert(err, IsNil)
		c.Check(mismatch, Equals, testCase.mismatch, Commentf("test case: %+v", testCase))

		restore()
	}
}

func (s *systemKeySuite) TestInterfaceSystemKeyMismatchVersions(c *C) {
	// we calculate v1
	s.AddCleanup(interfaces.MockSystemKey(`
{
"version":1,
"build-id": "7a94e9736c091b3984bd63f5aebfc883c4d859e0"
}`))
	// and the on-disk version is v2
	err := os.WriteFile(dirs.SnapSystemKeyFile, []byte(`
{
"version":2,
"build-id": "7a94e9736c091b3984bd63f5aebfc883c4d859e0"
}`), 0644)
	c.Assert(err, IsNil)

	extraData := interfaces.SystemKeyExtraData{}

	// when we encounter different versions we get the right error
	_, err = interfaces.SystemKeyMismatch(extraData)
	c.Assert(err, Equals, interfaces.ErrSystemKeyVersion)
}

func (s *systemKeySuite) TestStaticVersion(c *C) {
	// this is a static check to ensure we remember to bump the
	// version when we add fields
	//
	// *** IF THIS FAILS, YOU NEED TO BUMP THE VERSION BEFORE "FIXING" THIS ***
	var sk interfaces.SystemKey

	// XXX: this checks needs to become smarter once we remove or change
	// existing fields, in which case the version will gets a bump but the
	// number of fields decreases or remains unchanged
	c.Check(reflect.ValueOf(sk).NumField(), Equals, interfaces.SystemKeyVersion)

	c.Check(fmt.Sprintf("%+v", sk), Equals, "{"+strings.Join([]string{
		"Version:0",
		"BuildID:",
		"AppArmorFeatures:[]",
		"AppArmorParserMtime:0",
		"AppArmorParserFeatures:[]",
		"AppArmorPrompting:false",
		"NFSHome:false",
		"OverlayRoot:",
		"SecCompActions:[]",
		"SeccompCompilerVersion:",
		"CgroupVersion:",
	}, " ")+"}")
}

func (s *systemKeySuite) TestRecordedSystemKey(c *C) {
	_, err := interfaces.RecordedSystemKey()
	c.Check(err, Equals, interfaces.ErrSystemKeyMissing)

	restore := interfaces.MockSystemKey(`
{
"build-id": "7a94e9736c091b3984bd63f5aebfc883c4d859e0",
"apparmor-features": ["caps"]
}
`)
	defer restore()

	extraData := interfaces.SystemKeyExtraData{}

	c.Assert(interfaces.WriteSystemKey(extraData), IsNil)

	// just to ensure we really re-read it from the disk with RecordedSystemKey
	interfaces.MockSystemKey(`{"build-id":"foo"}`)

	key, err := interfaces.RecordedSystemKey()
	c.Assert(err, IsNil)

	sysKey, ok := key.(*interfaces.SystemKey)
	c.Assert(ok, Equals, true)
	c.Check(sysKey.BuildID, Equals, "7a94e9736c091b3984bd63f5aebfc883c4d859e0")
}

func (s *systemKeySuite) TestCurrentSystemKey(c *C) {
	restore := interfaces.MockSystemKey(`{"build-id": "7a94e9736c091b3984bd63f5aebfc883c4d859e0"}`)
	defer restore()

	key, err := interfaces.CurrentSystemKey()
	c.Assert(err, IsNil)
	sysKey, ok := key.(*interfaces.SystemKey)
	c.Assert(ok, Equals, true)
	c.Check(sysKey.BuildID, Equals, "7a94e9736c091b3984bd63f5aebfc883c4d859e0")
}

func (s *systemKeySuite) TestSystemKeysMatch(c *C) {
	_, err := interfaces.SystemKeysMatch(nil, nil)
	c.Check(err, ErrorMatches, `SystemKeysMatch: arguments are not system keys`)

	restore := interfaces.MockSystemKey(`{"build-id": "7a94e9736c091b3984bd63f5aebfc883c4d859e0"}`)
	defer restore()

	key1, err := interfaces.CurrentSystemKey()
	c.Assert(err, IsNil)

	_, err = interfaces.SystemKeysMatch(key1, nil)
	c.Check(err, ErrorMatches, `SystemKeysMatch: arguments are not system keys`)

	_, err = interfaces.SystemKeysMatch(nil, key1)
	c.Check(err, ErrorMatches, `SystemKeysMatch: arguments are not system keys`)

	interfaces.MockSystemKey(`{"build-id": "8888e9736c091b3984bd63f5aebfc883c4d85988"}`)
	key2, err := interfaces.CurrentSystemKey()
	c.Assert(err, IsNil)

	ok, err := interfaces.SystemKeysMatch(key1, key2)
	c.Assert(err, IsNil)
	c.Check(ok, Equals, false)

	key3, err := interfaces.CurrentSystemKey()
	c.Assert(err, IsNil)

	ok, err = interfaces.SystemKeysMatch(key2, key3)
	c.Assert(err, IsNil)
	c.Check(ok, Equals, true)
}

func (s *systemKeySuite) TestSystemKeysUnmarshalSame(c *C) {
	restore := interfaces.MockApparmorPromptingSupportedByFeatures(func(apparmorFeatures *apparmor.FeaturesSupported) (bool, string) {
		return true, ""
	})
	defer restore()
	appArmorPrompting := true
	extraData := interfaces.SystemKeyExtraData{
		AppArmorPrompting: appArmorPrompting,
	}

	// whitespace here simulates the serialization across HTTP, etc. that should
	// not trigger any differences
	// use a full system-key to fully test serialization, etc.
	systemKeyJSON := fmt.Sprintf(`
	{
		"apparmor-features": [
			"caps",
			"dbus",
			"domain",
			"file",
			"mount",
			"namespaces",
			"network",
			"network_v8",
			"policy",
			"ptrace",
			"query",
			"rlimit",
			"signal"
		],
		"apparmor-parser-features": [],
		"apparmor-parser-mtime": 1589907589,
		"apparmor-prompting": %t,
		"build-id": "cb94e5eeee4cf7ecda53f8308a984cb155b55732",
		"cgroup-version": "1",
		"nfs-home": false,
		"overlay-root": "",
		"seccomp-compiler-version": "e6e309ad8aee052e5aa695dfaa040328ae1559c5 2.4.3 9b218ef9a4e508dd8a7f848095cb8875d10a4bf28428ad81fdc3f8dac89108f7 bpf-actlog",
		"seccomp-features": [
			"allow",
			"errno",
			"kill_process",
			"kill_thread",
			"log",
			"trace",
			"trap",
			"user_notif"
		],
		"version": 11
	}`, appArmorPrompting)

	// write the mocked system key to disk
	restore = interfaces.MockSystemKey(systemKeyJSON)
	defer restore()
	err := interfaces.WriteSystemKey(extraData)
	c.Assert(err, IsNil)

	// now unmarshal the specific json to a system key object
	key1, err := interfaces.UnmarshalJSONSystemKey(bytes.NewBuffer([]byte(systemKeyJSON)))
	c.Assert(err, IsNil)

	// now read the system key from disk
	key2, err := interfaces.RecordedSystemKey()
	c.Assert(err, IsNil)

	// the two system-keys should be the same
	ok, err := interfaces.SystemKeysMatch(key1, key2)
	c.Assert(err, IsNil)
	c.Check(ok, Equals, true, Commentf("key1:%#v key2:%#v", key1, key2))
}

func (s *systemKeySuite) TestRemoveSystemKey(c *C) {
	extraData := interfaces.SystemKeyExtraData{}
	systemKeyJSON := `{}`

	// write the mocked system key to disk
	restore := interfaces.MockSystemKey(systemKeyJSON)
	defer restore()
	err := interfaces.WriteSystemKey(extraData)
	c.Assert(err, IsNil)

	c.Check(dirs.SnapSystemKeyFile, testutil.FilePresent)

	// remove the system key
	err = interfaces.RemoveSystemKey()
	c.Assert(err, IsNil)

	c.Check(dirs.SnapSystemKeyFile, testutil.FileAbsent)

	// also check that no error is returned when trying to remove system key
	// when it does not exist in the first place
	err = interfaces.RemoveSystemKey()
	c.Assert(err, IsNil)
}
