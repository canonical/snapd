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

var originalGetTimezone = getTimezone
var originalSetTimezone = setTimezone
var originalYamlMarshal = yamlMarshal

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
}

func (cts *ConfigTestSuite) TearDownTest(c *C) {
	getTimezone = originalGetTimezone
	setTimezone = originalSetTimezone
	yamlMarshal = originalYamlMarshal
}

// TestGet is a broad test, close enough to be an integration test.
func (cts *ConfigTestSuite) TestGet(c *C) {
	// TODO figure out if we care about exact output or just want valid yaml.
	expectedOutput := `config:
  ubuntu-core:
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
    timezone: America/Argentina/Mendoza
`

	rawConfig, err := Set(expected)
	c.Assert(err, IsNil)
	c.Assert(rawConfig, Equals, expected)
}

func (cts *ConfigTestSuite) TestSetInvalid(c *C) {
	input := `config:
  ubuntu-core:
    timezone America/Argentina/Mendoza
`

	rawConfig, err := Set(input)
	c.Assert(err, NotNil)
	c.Assert(rawConfig, Equals, "")
}

func (cts *ConfigTestSuite) TestNoChangeSet(c *C) {
	input := `config:
  ubuntu-core:
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
    timezone: America/Argentina/Mendoza
`

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
