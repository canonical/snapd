package coreconfig

import (
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	. "launchpad.net/gocheck"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

var (
	originalGetTimezone         = getTimezone
	originalSetTimezone         = setTimezone
	originalGetAutopilot        = getAutopilot
	originalSetAutopilot        = setAutopilot
	originalYamlMarshal         = yamlMarshal
	originalCmdEnableAutopilot  = cmdEnableAutopilot
	originalCmdDisableAutopilot = cmdDisableAutopilot
	originalCmdStartAutopilot   = cmdStartAutopilot
	originalCmdStopAutopilot    = cmdStopAutopilot
	originalCmdAutopilotEnabled = cmdAutopilotEnabled
	originalCmdSystemctl        = cmdSystemctl
)

type ConfigTestSuite struct {
	tempdir string
}

var _ = Suite(&ConfigTestSuite{})

func (cts *ConfigTestSuite) SetUpTest(c *C) {
	cts.tempdir = c.MkDir()
	tzPath := filepath.Join(cts.tempdir, "timezone")
	err := ioutil.WriteFile(tzPath, []byte("America/Argentina/Cordoba"), 0644)
	c.Assert(err, IsNil)
	os.Setenv(tzPathEnvironment, tzPath)

	cmdSystemctl = "/bin/sh"
	cmdAutopilotEnabled = []string{"-c", "echo disabled"}
	cmdEnableAutopilot = []string{"-c", "/bin/true"}
	cmdStartAutopilot = []string{"-c", "/bin/true"}
}

func (cts *ConfigTestSuite) TearDownTest(c *C) {
	getTimezone = originalGetTimezone
	setTimezone = originalSetTimezone
	getAutopilot = originalGetAutopilot
	setAutopilot = originalSetAutopilot
	yamlMarshal = originalYamlMarshal
	cmdEnableAutopilot = originalCmdEnableAutopilot
	cmdDisableAutopilot = originalCmdDisableAutopilot
	cmdStartAutopilot = originalCmdStartAutopilot
	cmdStopAutopilot = originalCmdStopAutopilot
	cmdAutopilotEnabled = originalCmdAutopilotEnabled
	cmdSystemctl = originalCmdSystemctl
}

// TestGet is a broad test, close enough to be an integration test for
// the defaults
func (cts *ConfigTestSuite) TestGet(c *C) {
	// TODO figure out if we care about exact output or just want valid yaml.
	expectedOutput := `config:
  ubuntu-core:
    autopilot: false
    timezone: America/Argentina/Cordoba
`

	rawConfig, err := Get()
	c.Assert(err, IsNil)
	c.Assert(rawConfig, Equals, expectedOutput)
}

// TestSet is a broad test, close enough to be an integration test.
func (cts *ConfigTestSuite) TestSet(c *C) {
	// TODO figure out if we care about exact output or just want valid yaml.
	expected := `config:
  ubuntu-core:
    autopilot: true
    timezone: America/Argentina/Mendoza
`

	cmdAutopilotEnabled = []string{"-c", "echo enabled"}
	rawConfig, err := Set(expected)
	c.Assert(err, IsNil)
	c.Assert(rawConfig, Equals, expected)
}

// TestSetTimezone is a broad test, close enough to be an integration test.
func (cts *ConfigTestSuite) TestSetTimezone(c *C) {
	// TODO figure out if we care about exact output or just want valid yaml.
	expected := `config:
  ubuntu-core:
    autopilot: false
    timezone: America/Argentina/Mendoza
`

	rawConfig, err := Set(expected)
	c.Assert(err, IsNil)
	c.Assert(rawConfig, Equals, expected)
}

// TestSetAutopilot is a broad test, close enough to be an integration test.
func (cts *ConfigTestSuite) TestSetAutopilot(c *C) {
	// TODO figure out if we care about exact output or just want valid yaml.
	expected := `config:
  ubuntu-core:
    autopilot: true
    timezone: America/Argentina/Cordoba
`

	enabled := false
	getAutopilot = func() (bool, error) { return enabled, nil }
	setAutopilot = func(state bool) error { enabled = state; return nil }

	rawConfig, err := Set(expected)
	c.Assert(err, IsNil)
	c.Assert(rawConfig, Equals, expected)
}

func (cts *ConfigTestSuite) TestSetInvalid(c *C) {
	input := `config:
  ubuntu-core:
    autopilot: false
    timezone America/Argentina/Mendoza
`

	rawConfig, err := Set(input)
	c.Assert(err, NotNil)
	c.Assert(rawConfig, Equals, "")
}

func (cts *ConfigTestSuite) TestNoChangeSet(c *C) {
	input := `config:
  ubuntu-core:
    autopilot: false
    timezone: America/Argentina/Cordoba
`

	rawConfig, err := Set(input)
	c.Assert(err, IsNil)
	c.Assert(rawConfig, Equals, input)
}

func (cts *ConfigTestSuite) TestNoEnvironmentTz(c *C) {
	os.Setenv(tzPathEnvironment, "")

	c.Assert(tzFile(), Equals, tzPathDefault)
}

func (cts *ConfigTestSuite) TestBadTzOnGet(c *C) {
	getTimezone = func() (string, error) { return "", errors.New("Bad mock tz") }

	rawConfig, err := Get()
	c.Assert(err, NotNil)
	c.Assert(rawConfig, Equals, "")
}

func (cts *ConfigTestSuite) TestBadTzOnSet(c *C) {
	getTimezone = func() (string, error) { return "", errors.New("Bad mock tz") }

	rawConfig, err := Set("config:")
	c.Assert(err, NotNil)
	c.Assert(rawConfig, Equals, "")
}

func (cts *ConfigTestSuite) TestErrorOnTzSet(c *C) {
	setTimezone = func(string) error { return errors.New("Bad mock tz") }

	input := `config:
  ubuntu-core:
    autopilot: false
    timezone: America/Argentina/Mendoza
`

	rawConfig, err := Set(input)
	c.Assert(err, NotNil)
	c.Assert(rawConfig, Equals, "")
}

func (cts *ConfigTestSuite) TestBadAutopilotOnGet(c *C) {
	getAutopilot = func() (bool, error) { return false, errors.New("Bad mock autopilot") }

	rawConfig, err := Get()
	c.Assert(err, NotNil)
	c.Assert(rawConfig, Equals, "")
}

func (cts *ConfigTestSuite) TestErrorOnAutopilotSet(c *C) {
	input := `config:
  ubuntu-core:
    autopilot: true
    timezone: America/Argentina/Mendoza
`

	enabled := false
	getAutopilot = func() (bool, error) { return enabled, nil }
	setAutopilot = func(state bool) error { enabled = state; return errors.New("setAutopilot error") }

	rawConfig, err := Set(input)
	c.Assert(err, NotNil)
	c.Assert(rawConfig, Equals, "")
}

func (cts *ConfigTestSuite) TestErrorOnUnmarshal(c *C) {
	yamlMarshal = func(interface{}) ([]byte, error) { return []byte{}, errors.New("Mock unmarhal error") }

	setTimezone = func(string) error { return errors.New("Bad mock tz") }

	rawConfig, err := Get()
	c.Assert(err, NotNil)
	c.Assert(rawConfig, Equals, "")
}

func (cts *ConfigTestSuite) TestInvalidTzFile(c *C) {
	os.Setenv(tzPathEnvironment, "file/does/not/exist")

	tz, err := getTimezone()
	c.Assert(err, NotNil)
	c.Assert(tz, Equals, "")
}

func (cts *ConfigTestSuite) TestInvalidAutopilotUnitStatus(c *C) {
	cmdAutopilotEnabled = []string{"-c", "echo unkown"}

	autopilot, err := getAutopilot()
	c.Assert(err, NotNil)
	c.Assert(autopilot, Equals, false)
}

func (cts *ConfigTestSuite) TestInvalidAutopilotExitStatus(c *C) {
	cmdAutopilotEnabled = []string{"-c", "exit 2"}

	autopilot, err := getAutopilot()
	c.Assert(err, NotNil)
	c.Assert(autopilot, Equals, false)
}

func (cts *ConfigTestSuite) TestInvalidGetAutopilotCommand(c *C) {
	cmdSystemctl = "/bin/sh"
	cmdAutopilotEnabled = []string{"-c", "/bin/false"}

	autopilot, err := getAutopilot()
	c.Assert(err, NotNil)
	c.Assert(autopilot, Equals, false)
}

func (cts *ConfigTestSuite) TestSetAutopilots(c *C) {
	cmdSystemctl = "/bin/sh"

	// no errors
	c.Assert(setAutopilot(true), IsNil)

	// enable cases
	cmdEnableAutopilot = []string{"-c", "/bin/true"}
	cmdStartAutopilot = []string{"-c", "/bin/false"}
	c.Assert(setAutopilot(true), NotNil)

	cmdEnableAutopilot = []string{"-c", "/bin/false"}
	c.Assert(setAutopilot(true), NotNil)

	// disable cases
	cmdStopAutopilot = []string{"-c", "/bin/true"}
	cmdDisableAutopilot = []string{"-c", "/bin/false"}
	c.Assert(setAutopilot(false), NotNil)

	cmdStopAutopilot = []string{"-c", "/bin/false"}
	c.Assert(setAutopilot(false), NotNil)
}
