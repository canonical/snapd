// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package coreconfig

import (
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/systemd"

	. "gopkg.in/check.v1"
)

// Hook up check.v1 into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

var (
	originalGetTimezone          = getTimezone
	originalSetTimezone          = setTimezone
	originalGetAutoUpdate        = getAutoUpdate
	originalSetAutoUpdate        = setAutoUpdate
	originalGetHostname          = getHostname
	originalSetHostname          = setHostname
	originalSyscallSethostname   = syscallSethostname
	originalYamlMarshal          = yamlMarshal
	originalCmdEnableAutoUpdate  = cmdEnableAutoUpdate
	originalCmdDisableAutoUpdate = cmdDisableAutoUpdate
	originalCmdStartAutoUpdate   = cmdStartAutoUpdate
	originalCmdStopAutoUpdate    = cmdStopAutoUpdate
	originalCmdAutoUpdateEnabled = cmdAutoUpdateEnabled
	originalCmdSystemctl         = cmdSystemctl
	originalHostnamePath         = hostnamePath
	originalModprobePath         = modprobePath
	originalModulesPath          = modulesPath
	originalInterfacesRoot       = interfacesRoot
	originalPppRoot              = pppRoot
	originalWatchdogStartupPath  = watchdogStartupPath
	originalWatchdogConfigPath   = watchdogConfigPath
	originalTzZoneInfoTarget     = tzZoneInfoTarget
)

type ConfigTestSuite struct {
	tempdir string
	// sysctl fakes
	sysctlcmd    func(...string) ([]byte, error)
	sysctlargses [][]string
	sysctlerr    error
}

var _ = Suite(&ConfigTestSuite{})

func (cts *ConfigTestSuite) SetUpTest(c *C) {
	cts.tempdir = c.MkDir()
	tzPath := filepath.Join(cts.tempdir, "timezone")
	err := ioutil.WriteFile(tzPath, []byte("America/Argentina/Cordoba"), 0644)
	c.Assert(err, IsNil)
	os.Setenv(tzPathEnvironment, tzPath)

	cmdSystemctl = "/bin/sh"
	cmdAutoUpdateEnabled = []string{"-c", "echo disabled"}
	cmdEnableAutoUpdate = []string{"-c", "/bin/true"}
	cmdStartAutoUpdate = []string{"-c", "/bin/true"}

	hostname := "testhost"
	getHostname = func() (string, error) { return hostname, nil }
	setHostname = func(host string) error {
		hostname = host
		return nil
	}
	tzZoneInfoTarget = filepath.Join(c.MkDir(), "localtime")

	interfacesRoot = c.MkDir() + "/"
	pppRoot = c.MkDir() + "/"
	watchdogConfigPath = filepath.Join(c.MkDir(), "watchdog-config")
	watchdogStartupPath = filepath.Join(c.MkDir(), "watchdog-startup")

	cts.sysctlerr = nil
	cts.sysctlargses = nil
	cts.sysctlcmd = systemd.SystemctlCmd
	systemd.SystemctlCmd = cts.run
}

func (cts *ConfigTestSuite) run(args ...string) ([]byte, error) {
	cts.sysctlargses = append(cts.sysctlargses, args)

	return nil, cts.sysctlerr
}

func (cts *ConfigTestSuite) TearDownTest(c *C) {
	getTimezone = originalGetTimezone
	setTimezone = originalSetTimezone
	getAutoUpdate = originalGetAutoUpdate
	setAutoUpdate = originalSetAutoUpdate
	getHostname = originalGetHostname
	setHostname = originalSetHostname
	syscallSethostname = originalSyscallSethostname
	hostnamePath = originalHostnamePath
	yamlMarshal = originalYamlMarshal
	cmdEnableAutoUpdate = originalCmdEnableAutoUpdate
	cmdDisableAutoUpdate = originalCmdDisableAutoUpdate
	cmdStartAutoUpdate = originalCmdStartAutoUpdate
	cmdStopAutoUpdate = originalCmdStopAutoUpdate
	cmdAutoUpdateEnabled = originalCmdAutoUpdateEnabled
	cmdSystemctl = originalCmdSystemctl
	modprobePath = originalModprobePath
	modulesPath = originalModulesPath
	interfacesRoot = originalInterfacesRoot
	pppRoot = originalPppRoot
	watchdogStartupPath = originalWatchdogStartupPath
	watchdogConfigPath = originalWatchdogConfigPath
	tzZoneInfoTarget = originalTzZoneInfoTarget

	systemd.SystemctlCmd = cts.sysctlcmd
}

// TestGet is a broad test, close enough to be an integration test for
// the defaults
func (cts *ConfigTestSuite) TestGet(c *C) {
	// TODO figure out if we care about exact output or just want valid yaml.
	expectedOutput := `config:
  ubuntu-core:
    autoupdate: false
    timezone: America/Argentina/Cordoba
    hostname: testhost
    modprobe: ""
`

	rawConfig, err := Get()
	c.Assert(err, IsNil)
	c.Assert(string(rawConfig), Equals, expectedOutput)
}

// TestSet is a broad test, close enough to be an integration test.
func (cts *ConfigTestSuite) TestSet(c *C) {
	// TODO figure out if we care about exact output or just want valid yaml.
	expected := `config:
  ubuntu-core:
    autoupdate: true
    timezone: America/Argentina/Mendoza
    hostname: testhost
    modprobe: ""
`

	cmdAutoUpdateEnabled = []string{"-c", "echo enabled"}
	rawConfig, err := Set([]byte(expected))
	c.Assert(err, IsNil)
	c.Assert(string(rawConfig), Equals, expected)

	// systemctl hasn't been called
	c.Check(cts.sysctlargses, HasLen, 0)
}

func (cts *ConfigTestSuite) TestSetBadValueDoesNotPanic(c *C) {
	for _, s := range []string{
		"",
		"\n",
		"config:\n",
		"config:\n ubuntu-core:\n",
	} {
		_, err := Set([]byte(s))
		c.Assert(err, Equals, ErrInvalidConfig)
	}

	// systemctl hasn't been called
	c.Check(cts.sysctlargses, HasLen, 0)
}

// TestSetTimezone is a broad test, close enough to be an integration test.
func (cts *ConfigTestSuite) TestSetTimezone(c *C) {
	// TODO figure out if we care about exact output or just want valid yaml.
	expected := `config:
  ubuntu-core:
    autoupdate: false
    timezone: America/Argentina/Mendoza
    hostname: testhost
    modprobe: ""
`

	rawConfig, err := Set([]byte(expected))
	c.Assert(err, IsNil)
	c.Assert(string(rawConfig), Equals, expected)
	c.Assert(osutil.FileExists(tzZoneInfoTarget), Equals, true)

	// systemctl hasn't been called
	c.Check(cts.sysctlargses, HasLen, 0)
}

func (cts *ConfigTestSuite) TestSetTimezoneAlreadyExists(c *C) {
	// TODO figure out if we care about exact output or just want valid yaml.
	expected := `config:
  ubuntu-core:
    autoupdate: false
    timezone: America/Argentina/Mendoza
    hostname: testhost
    modprobe: ""
`
	canary := []byte("Ni Ni Ni")
	err := ioutil.WriteFile(tzZoneInfoTarget, canary, 0644)
	c.Assert(err, IsNil)

	rawConfig, err := Set([]byte(expected))
	c.Assert(err, IsNil)
	c.Assert(string(rawConfig), Equals, expected)
	content, err := ioutil.ReadFile(tzZoneInfoTarget)
	c.Assert(err, IsNil)
	c.Assert(content, Not(DeepEquals), []byte(canary))

	// systemctl hasn't been called
	c.Check(cts.sysctlargses, HasLen, 0)
}

// TestSetAutoUpdate is a broad test, close enough to be an integration test.
func (cts *ConfigTestSuite) TestSetAutoUpdate(c *C) {
	// TODO figure out if we care about exact output or just want valid yaml.
	expected := `config:
  ubuntu-core:
    autoupdate: true
    timezone: America/Argentina/Cordoba
    hostname: testhost
    modprobe: ""
`

	enabled := false
	getAutoUpdate = func() (bool, error) { return enabled, nil }
	setAutoUpdate = func(state bool) error { enabled = state; return nil }

	rawConfig, err := Set([]byte(expected))
	c.Assert(err, IsNil)
	c.Assert(string(rawConfig), Equals, expected)

	// systemctl hasn't been called
	c.Check(cts.sysctlargses, HasLen, 0)
}

// TestSetHostname is a broad test, close enough to be an integration test.
func (cts *ConfigTestSuite) TestSetHostname(c *C) {
	expected := `config:
  ubuntu-core:
    autoupdate: false
    timezone: America/Argentina/Cordoba
    hostname: NEWtesthost
    modprobe: ""
`

	rawConfig, err := Set([]byte(expected))
	c.Assert(err, IsNil)
	c.Assert(string(rawConfig), Equals, expected)

	// systemctl hasn't been called
	c.Check(cts.sysctlargses, HasLen, 0)
}

func (cts *ConfigTestSuite) TestSetInvalid(c *C) {
	input := `config:
  ubuntu-core:
    autoupdate: false
    timezone America/Argentina/Mendoza
    hostname: testhost
    modprobe: ""
`

	rawConfig, err := Set([]byte(input))
	c.Assert(err, NotNil)
	c.Assert(rawConfig, IsNil)

	// systemctl hasn't been called
	c.Check(cts.sysctlargses, HasLen, 0)
}

func (cts *ConfigTestSuite) TestNoChangeSet(c *C) {
	input := `config:
  ubuntu-core:
    autoupdate: false
    timezone: America/Argentina/Cordoba
    hostname: testhost
    modprobe: ""
`

	rawConfig, err := Set([]byte(input))
	c.Assert(err, IsNil)
	c.Assert(string(rawConfig), Equals, input)

	// systemctl hasn't been called
	c.Check(cts.sysctlargses, HasLen, 0)
}

func (cts *ConfigTestSuite) TestPartialInput(c *C) {
	expected := `config:
  ubuntu-core:
    autoupdate: false
    timezone: America/Argentina/Cordoba
    hostname: testhost
    modprobe: ""
`

	input := `config:
  ubuntu-core:
    autoupdate: false
    timezone: America/Argentina/Cordoba
    modprobe: ""
`

	rawConfig, err := Set([]byte(input))
	c.Assert(err, IsNil)
	c.Assert(string(rawConfig), Equals, expected)

	// systemctl hasn't been called
	c.Check(cts.sysctlargses, HasLen, 0)
}

func (cts *ConfigTestSuite) TestNoEnvironmentTz(c *C) {
	os.Setenv(tzPathEnvironment, "")

	c.Assert(tzFile(), Equals, tzPathDefault)
}

func (cts *ConfigTestSuite) TestBadTzOnGet(c *C) {
	getTimezone = func() (string, error) { return "", errors.New("Bad mock tz") }

	rawConfig, err := Get()
	c.Assert(err, NotNil)
	c.Assert(rawConfig, IsNil)
}

func (cts *ConfigTestSuite) TestBadTzOnSet(c *C) {
	getTimezone = func() (string, error) { return "", errors.New("Bad mock tz") }

	rawConfig, err := Set([]byte("config:"))
	c.Assert(err, NotNil)
	c.Assert(rawConfig, IsNil)

	// systemctl hasn't been called
	c.Check(cts.sysctlargses, HasLen, 0)
}

func (cts *ConfigTestSuite) TestErrorOnTzSet(c *C) {
	setTimezone = func(string) error { return errors.New("Bad mock tz") }

	input := `config:
  ubuntu-core:
    autoupdate: false
    timezone: America/Argentina/Mendoza
    hostname: testhost
    modprobe: ""
`

	rawConfig, err := Set([]byte(input))
	c.Assert(err, NotNil)
	c.Assert(rawConfig, IsNil)

	// systemctl hasn't been called
	c.Check(cts.sysctlargses, HasLen, 0)
}

func (cts *ConfigTestSuite) TestBadAutoUpdateOnGet(c *C) {
	getAutoUpdate = func() (bool, error) { return false, errors.New("Bad mock autoUpdate") }

	rawConfig, err := Get()
	c.Assert(err, NotNil)
	c.Assert(rawConfig, IsNil)
}

func (cts *ConfigTestSuite) TestErrorOnAutoUpdateSet(c *C) {
	input := `config:
  ubuntu-core:
    autoupdate: true
    timezone: America/Argentina/Mendoza
    hostname: testhost
    modprobe: ""
`

	enabled := false
	getAutoUpdate = func() (bool, error) { return enabled, nil }
	setAutoUpdate = func(state bool) error { enabled = state; return errors.New("setAutoUpdate error") }

	rawConfig, err := Set([]byte(input))
	c.Assert(err, NotNil)
	c.Assert(rawConfig, IsNil)

	// systemctl hasn't been called
	c.Check(cts.sysctlargses, HasLen, 0)
}

func (cts *ConfigTestSuite) TestErrorOnSetHostname(c *C) {
	input := `config:
  ubuntu-core:
    autoupdate: false
    timezone: America/Argentina/Cordoba
    hostname: NEWtesthost
    modprobe: ""
`

	setHostname = func(string) error { return errors.New("this is bad") }

	rawConfig, err := Set([]byte(input))
	c.Assert(err, NotNil)
	c.Assert(rawConfig, IsNil)

	// systemctl hasn't been called
	c.Check(cts.sysctlargses, HasLen, 0)
}

func (cts *ConfigTestSuite) TestErrorOnGetHostname(c *C) {
	input := `config:
  ubuntu-core:
    autoupdate: false
    timezone: America/Argentina/Cordoba
    hostname: NEWtesthost
    modprobe: ""
`

	getHostname = func() (string, error) { return "", errors.New("this is bad") }

	rawConfig, err := Set([]byte(input))
	c.Assert(err, NotNil)
	c.Assert(rawConfig, IsNil)

	// systemctl hasn't been called
	c.Check(cts.sysctlargses, HasLen, 0)
}

func (cts *ConfigTestSuite) TestErrorOnUnmarshal(c *C) {
	yamlMarshal = func(interface{}) ([]byte, error) { return []byte{}, errors.New("Mock unmarhal error") }

	setTimezone = func(string) error { return errors.New("Bad mock tz") }

	rawConfig, err := Get()
	c.Assert(err, NotNil)
	c.Assert(rawConfig, IsNil)
}

func (cts *ConfigTestSuite) TestInvalidTzFile(c *C) {
	os.Setenv(tzPathEnvironment, "file/does/not/exist")

	tz, err := getTimezone()
	c.Assert(err, NotNil)
	c.Assert(tz, Equals, "")
}

func (cts *ConfigTestSuite) TestInvalidAutoUpdateUnitStatus(c *C) {
	cmdAutoUpdateEnabled = []string{"-c", "echo unkown"}

	autoUpdate, err := getAutoUpdate()
	c.Assert(err, NotNil)
	c.Assert(autoUpdate, Equals, false)
}

func (cts *ConfigTestSuite) TestInvalidAutoUpdateExitStatus(c *C) {
	cmdAutoUpdateEnabled = []string{"-c", "exit 2"}

	autoUpdate, err := getAutoUpdate()
	c.Assert(err, NotNil)
	c.Assert(autoUpdate, Equals, false)
}

func (cts *ConfigTestSuite) TestInvalidGetAutoUpdateCommand(c *C) {
	cmdSystemctl = "/bin/sh"
	cmdAutoUpdateEnabled = []string{"-c", "/bin/false"}

	autoUpdate, err := getAutoUpdate()
	c.Assert(err, NotNil)
	c.Assert(autoUpdate, Equals, false)
}

func (cts *ConfigTestSuite) TestSetAutoUpdates(c *C) {
	cmdSystemctl = "/bin/sh"

	// no errors
	c.Assert(setAutoUpdate(true), IsNil)

	// enable cases
	cmdEnableAutoUpdate = []string{"-c", "/bin/true"}
	cmdStartAutoUpdate = []string{"-c", "/bin/false"}
	c.Assert(setAutoUpdate(true), NotNil)

	cmdEnableAutoUpdate = []string{"-c", "/bin/false"}
	c.Assert(setAutoUpdate(true), NotNil)

	// disable cases
	cmdStopAutoUpdate = []string{"-c", "/bin/true"}
	cmdDisableAutoUpdate = []string{"-c", "/bin/false"}
	c.Assert(setAutoUpdate(false), NotNil)

	cmdStopAutoUpdate = []string{"-c", "/bin/false"}
	c.Assert(setAutoUpdate(false), NotNil)
}

func (cts *ConfigTestSuite) TestSetHostnameImpl(c *C) {
	syscallSethostname = func([]byte) error { return nil }
	hostnamePath = filepath.Join(c.MkDir(), "hostname")
	setHostname = originalSetHostname

	err := setHostname("newhostname")
	c.Assert(err, IsNil)

	contents, err := ioutil.ReadFile(hostnamePath)
	c.Assert(err, IsNil)
	c.Assert(string(contents), Equals, "newhostname")
}

func (cts *ConfigTestSuite) TestSetHostnameImplErrors(c *C) {
	expectedErr := errors.New("what happened?")
	syscallSethostname = func([]byte) error { return expectedErr }
	setHostname = originalSetHostname

	err := setHostname("newhostname")
	c.Assert(err, DeepEquals, expectedErr)
}

func (cts *ConfigTestSuite) TestModprobe(c *C) {
	modprobePath = filepath.Join(c.MkDir(), "test.conf")

	err := setModprobe("blacklist floppy")
	c.Assert(err, IsNil)

	modprobe, err := getModprobe()
	c.Assert(err, IsNil)
	c.Assert(modprobe, Equals, "blacklist floppy")
}

func (cts *ConfigTestSuite) TestModprobeYaml(c *C) {
	modprobePath = filepath.Join(c.MkDir(), "test.conf")

	// systemctl hadn't been called
	c.Check(cts.sysctlargses, HasLen, 0)

	input := `config:
  ubuntu-core:
    modprobe: |
      blacklist floppy
      softdep mlx4_core post: mlx4_en
`
	_, err := Set([]byte(input))
	c.Assert(err, IsNil)

	// systemctl was called
	c.Check(cts.sysctlargses, DeepEquals, [][]string{{"restart", "--no-block", "systemd-modules-load.service"}})

	// ensure it's really there
	content, err := ioutil.ReadFile(modprobePath)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, "blacklist floppy\nsoftdep mlx4_core post: mlx4_en\n")
}

func (cts *ConfigTestSuite) TestModules(c *C) {
	modulesPath = filepath.Join(c.MkDir(), "test.conf")

	modules, err := getModules()
	c.Assert(err, IsNil)
	c.Check(modules, HasLen, 0)

	c.Assert(setModules([]string{"foo"}), IsNil)

	modules, err = getModules()
	c.Assert(err, IsNil)
	c.Check(modules, DeepEquals, []string{"foo"})

	c.Assert(setModules([]string{"bar"}), IsNil)

	modules, err = getModules()
	c.Assert(err, IsNil)
	c.Check(modules, DeepEquals, []string{"bar", "foo"})

	c.Assert(setModules([]string{"-foo"}), IsNil)

	modules, err = getModules()
	c.Assert(err, IsNil)
	c.Check(modules, DeepEquals, []string{"bar"})
}

func (cts *ConfigTestSuite) TestModulesRemoveAbsent(c *C) {
	modulesPath = filepath.Join(c.MkDir(), "test.conf")

	c.Assert(setModules([]string{"foo"}), IsNil)
	c.Assert(setModules([]string{"-bar"}), IsNil)

	modules, err := getModules()
	c.Assert(err, IsNil)
	c.Check(modules, DeepEquals, []string{"foo"})
}

func (cts *ConfigTestSuite) TestModulesRemoveEmpty(c *C) {
	modulesPath = filepath.Join(c.MkDir(), "test.conf")

	c.Assert(setModules([]string{"foo"}), IsNil)
	c.Assert(setModules([]string{"-"}), IsNil)

	modules, err := getModules()
	c.Assert(err, IsNil)
	c.Check(modules, DeepEquals, []string{"foo"})
}

func (cts *ConfigTestSuite) TestModulesRemoveBlank(c *C) {
	modulesPath = filepath.Join(c.MkDir(), "test.conf")

	c.Assert(setModules([]string{"foo"}), IsNil)
	c.Assert(setModules([]string{"- "}), IsNil)

	modules, err := getModules()
	c.Assert(err, IsNil)
	c.Check(modules, DeepEquals, []string{"foo"})
}

func (cts *ConfigTestSuite) TestModulesAddDupe(c *C) {
	modulesPath = filepath.Join(c.MkDir(), "test.conf")

	c.Assert(setModules([]string{"foo"}), IsNil)
	c.Assert(setModules([]string{"foo"}), IsNil)

	modules, err := getModules()
	c.Assert(err, IsNil)
	c.Check(modules, DeepEquals, []string{"foo"})
}

func (cts *ConfigTestSuite) TestModulesAddEmpty(c *C) {
	modulesPath = filepath.Join(c.MkDir(), "test.conf")

	c.Assert(setModules([]string{"foo"}), IsNil)
	c.Assert(setModules([]string{""}), IsNil)

	modules, err := getModules()
	c.Assert(err, IsNil)
	c.Check(modules, DeepEquals, []string{"foo"})
}

func (cts *ConfigTestSuite) TestModulesAddBlank(c *C) {
	modulesPath = filepath.Join(c.MkDir(), "test.conf")

	c.Assert(setModules([]string{"foo"}), IsNil)
	c.Assert(setModules([]string{" "}), IsNil)

	modules, err := getModules()
	c.Assert(err, IsNil)
	c.Check(modules, DeepEquals, []string{"foo"})
}

func (cts *ConfigTestSuite) TestModulesHasWarning(c *C) {
	modulesPath = filepath.Join(c.MkDir(), "test.conf")

	c.Assert(setModules([]string{"foo"}), IsNil)

	bs, err := ioutil.ReadFile(modulesPath)
	c.Assert(err, IsNil)
	c.Check(string(bs), Matches, `(?s).*DO NOT EDIT.*`)
}

func (cts *ConfigTestSuite) TestModulesIsKind(c *C) {
	modulesPath = filepath.Join(c.MkDir(), "test.conf")
	c.Assert(ioutil.WriteFile(modulesPath, []byte(`# hello
# this is what happens when somebody comes and edits the file
# just to be sure
; modules-load.d(5) says comments can also start with a ;
  ; actually not even start
  # it's the first non-whitespace that counts
#    also here's an empty line:

# and here's a module with spurious whitespace:
  oops
# that is all. Have a good day.
`), 0644), IsNil)

	modules, err := getModules()
	c.Check(err, IsNil)
	c.Check(modules, DeepEquals, []string{"oops"})
}

func (cts *ConfigTestSuite) TestModulesYaml(c *C) {
	modulesPath = filepath.Join(c.MkDir(), "test.conf")

	c.Assert(setModules([]string{"foo"}), IsNil)

	cfg, err := newSystemConfig()
	c.Assert(err, IsNil)
	c.Check(cfg.Modules, DeepEquals, []string{"foo"})

	// systemctl hadn't been called
	c.Check(cts.sysctlargses, HasLen, 0)

	input := `config:
  ubuntu-core:
    load-kernel-modules: [-foo, bar]
`
	_, err = Set([]byte(input))
	c.Assert(err, IsNil)

	// systemctl was called
	c.Check(cts.sysctlargses, DeepEquals, [][]string{{"restart", "--no-block", "systemd-modules-load.service"}})

	// ensure it's really there
	content, err := ioutil.ReadFile(modulesPath)
	c.Assert(err, IsNil)
	c.Assert(string(content), Matches, `(?sm).*^bar$.*`)

	modules, err := getModules()
	c.Assert(err, IsNil)
	c.Check(modules, DeepEquals, []string{"bar"})
}

func (cts *ConfigTestSuite) TestModulesErrorWrite(c *C) {
	// modulesPath is not writable, but only notexist read error
	modulesPath = filepath.Join(c.MkDir(), "not-there", "test.conf")

	c.Check(setModules([]string{"bar"}), NotNil)

	input := `config:
  ubuntu-core:
    load-kernel-modules: [foo]
`
	_, err := Set([]byte(input))
	c.Check(err, NotNil)

	_, err = getModules()
	c.Check(err, IsNil)

	_, err = newSystemConfig()
	c.Check(err, IsNil)
}

func (cts *ConfigTestSuite) TestModulesErrorRW(c *C) {
	modulesPath = c.MkDir()

	modules, err := getModules()
	c.Check(err, NotNil)
	c.Check(modules, HasLen, 0)
	c.Check(setModules([]string{"bar"}), NotNil)

	_, err = newSystemConfig()
	c.Check(err, NotNil)

	_, err = Set([]byte("config: {ubuntu-core: {modules: [foo]}}"))
	c.Check(err, NotNil)
}

func (cts *ConfigTestSuite) TestNetworkGet(c *C) {
	path := filepath.Join(interfacesRoot, "eth0")
	content := "auto eth0"
	err := ioutil.WriteFile(path, []byte(content), 0644)
	c.Assert(err, IsNil)

	nc, err := getInterfaces()
	c.Assert(err, IsNil)
	c.Assert(nc, DeepEquals, []passthroughConfig{
		{Name: "eth0", Content: "auto eth0"},
	})
}

func (cts *ConfigTestSuite) TestNetworkSet(c *C) {
	nc := []passthroughConfig{
		{Name: "eth0", Content: "auto eth0"},
	}
	path := filepath.Join(interfacesRoot, nc[0].Name)
	err := setInterfaces(nc)
	c.Assert(err, IsNil)
	content, err := ioutil.ReadFile(path)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, nc[0].Content)
}

func (cts *ConfigTestSuite) TestNetworkSetEmptyRemoves(c *C) {
	path := filepath.Join(interfacesRoot, "eth0")
	content := "auto eth0"
	err := ioutil.WriteFile(path, []byte(content), 0644)
	c.Assert(err, IsNil)

	// empty content removes
	nc := []passthroughConfig{
		{Name: "eth0", Content: ""},
	}
	err = setInterfaces(nc)
	c.Assert(err, IsNil)
	_, err = ioutil.ReadFile(path)
	c.Assert(osutil.FileExists(path), Equals, false)
}

func (cts *ConfigTestSuite) TestPppGet(c *C) {
	path := filepath.Join(pppRoot, "chap-secrets")
	content := "password"
	err := ioutil.WriteFile(path, []byte(content), 0644)
	c.Assert(err, IsNil)

	nc, err := getPPP()
	c.Assert(err, IsNil)
	c.Assert(nc, DeepEquals, []passthroughConfig{
		{Name: "chap-secrets", Content: "password"},
	})
}

func (cts *ConfigTestSuite) TestPppSet(c *C) {
	nc := []passthroughConfig{
		{Name: "chap-secrets", Content: "another secret"},
	}
	path := filepath.Join(pppRoot, nc[0].Name)
	err := setPPP(nc)
	c.Assert(err, IsNil)
	content, err := ioutil.ReadFile(path)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, nc[0].Content)
}

func (cts *ConfigTestSuite) TestNetworkSetViaYaml(c *C) {
	input := `
config:
  ubuntu-core:
    network:
      interfaces:
        - name: eth0
          content: auto dhcp
`
	_, err := Set([]byte(input))
	c.Assert(err, IsNil)

	// ensure it's really there
	content, err := ioutil.ReadFile(filepath.Join(interfacesRoot, "eth0"))
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, "auto dhcp")

	// systemctl hasn't been called
	c.Check(cts.sysctlargses, HasLen, 0)
}

func (cts *ConfigTestSuite) TestPPPSetViaYaml(c *C) {
	modprobePath = filepath.Join(c.MkDir(), "test.conf")

	input := `
config:
  ubuntu-core:
    network:
      ppp:
        - name: chap-secret
          content: password
`
	_, err := Set([]byte(input))
	c.Assert(err, IsNil)

	// ensure it's really there
	content, err := ioutil.ReadFile(filepath.Join(pppRoot, "chap-secret"))
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, "password")

	// systemctl hasn't been called
	c.Check(cts.sysctlargses, HasLen, 0)
}

func (cts *ConfigTestSuite) TestPassthroughConfigEqual(c *C) {
	a := []passthroughConfig{
		{Name: "key", Content: "value"},
	}
	b := []passthroughConfig{
		{Name: "key", Content: "value"},
	}
	c.Assert(passthroughEqual(a, b), Equals, true)
}

func (cts *ConfigTestSuite) TestPassthroughConfigNotEqualDifferentSize(c *C) {
	a := []passthroughConfig{}
	b := []passthroughConfig{
		{Name: "key", Content: "value"},
	}
	c.Assert(passthroughEqual(a, b), Equals, false)
}

func (cts *ConfigTestSuite) TestPassthroughConfigNotEqualDifferentKeys(c *C) {
	a := []passthroughConfig{
		{Name: "key", Content: "value"},
	}
	b := []passthroughConfig{
		{Name: "other-key", Content: "value"},
	}
	c.Assert(passthroughEqual(a, b), Equals, false)
}

func (cts *ConfigTestSuite) TestWatchdogGet(c *C) {
	startup := "# some startup watchdog config"
	err := ioutil.WriteFile(watchdogStartupPath, []byte(startup), 0644)
	c.Assert(err, IsNil)

	config := "# some watchdog config"
	err = ioutil.WriteFile(watchdogConfigPath, []byte(config), 0644)
	c.Assert(err, IsNil)

	wc, err := getWatchdog()
	c.Assert(err, IsNil)
	c.Assert(wc, DeepEquals, &watchdogConfig{
		Startup: startup, Config: config,
	})
}

func (cts *ConfigTestSuite) TestWatchdogSet(c *C) {
	wc := &watchdogConfig{
		Startup: "startup", Config: "secret",
	}
	err := setWatchdog(wc)
	c.Assert(err, IsNil)

	content, err := ioutil.ReadFile(watchdogStartupPath)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, wc.Startup)

	content, err = ioutil.ReadFile(watchdogConfigPath)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, wc.Config)
}

func (cts *ConfigTestSuite) TestWatchdogSetViaYaml(c *C) {
	input := `
config:
  ubuntu-core:
    watchdog:
      startup: some startup
      config: some config
`
	_, err := Set([]byte(input))
	c.Assert(err, IsNil)

	// ensure it's really there
	content, err := ioutil.ReadFile(watchdogStartupPath)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, "some startup")

	content, err = ioutil.ReadFile(watchdogConfigPath)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, "some config")

	// systemctl hasn't been called
	c.Check(cts.sysctlargses, HasLen, 0)
}
