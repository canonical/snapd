package snappy

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	. "launchpad.net/gocheck"
)

func MyTest(t *testing.T) { TestingT(t) }

type HTestSuite struct{}

var _ = Suite(&HTestSuite{})

func (ts *HTestSuite) TestUnpack(c *C) {

	// setup tmpdir
	tmpdir := c.MkDir()
	tmpfile := filepath.Join(tmpdir, "foo.tar.gz")

	// ok, slightly silly
	path := "/etc/fstab"

	// create test data
	cmd := exec.Command("tar", "cvzf", tmpfile, path)
	output, err := cmd.CombinedOutput()
	c.Assert(err, IsNil)
	if !strings.Contains(string(output), "/etc/fstab") {
		c.Error("Can not find expected output from tar")
	}

	// unpack
	unpackdir := filepath.Join(tmpdir, "t")
	err = unpackTar(tmpfile, unpackdir)
	c.Assert(err, IsNil)

	_, err = os.Open(filepath.Join(tmpdir, "t/etc/fstab"))
	c.Assert(err, IsNil)
}

func (ts *HTestSuite) TestGetMapFromValidYaml(c *C) {
	m, err := getMapFromYaml([]byte("name: value"))
	c.Assert(err, IsNil)
	me := map[string]interface{}{"name": "value"}
	if !reflect.DeepEqual(m, me) {
		c.Error(fmt.Sprintf("Unexpected map %v != %v", m, me))
	}
}

func (ts *HTestSuite) TestGetMapFromInvalidYaml(c *C) {
	_, err := getMapFromYaml([]byte("%lala%"))
	c.Assert(err, NotNil)
}

func (ts *HTestSuite) TestArchitectue(c *C) {
	goarch = "arm"
	c.Check(Architecture(), Equals, "armhf")

	goarch = "amd64"
	c.Check(Architecture(), Equals, "amd64")

	goarch = "386"
	c.Check(Architecture(), Equals, "i386")
}

func (ts *HTestSuite) TestChdir(c *C) {
	tmpdir := c.MkDir()

	cwd, err := os.Getwd()
	c.Assert(err, IsNil)
	c.Assert(cwd, Not(Equals), tmpdir)
	chDir(tmpdir, func() {
		cwd, err := os.Getwd()
		c.Assert(err, IsNil)
		c.Assert(cwd, Equals, tmpdir)
	})
}

func (ts *HTestSuite) TestExitCode(c *C) {
	cmd := exec.Command("true")
	err := cmd.Run()
	c.Assert(err, IsNil)

	cmd = exec.Command("false")
	err = cmd.Run()
	c.Assert(err, NotNil)
	e, err := exitCode(err)
	c.Assert(err, IsNil)
	c.Assert(e, Equals, 1)

	cmd = exec.Command("sh", "-c", "exit 7")
	err = cmd.Run()
	e, err = exitCode(err)
	c.Assert(e, Equals, 7)

	// ensure that non exec.ExitError values give a error
	_, err = os.Stat("/random/file/that/is/not/there")
	c.Assert(err, NotNil)
	_, err = exitCode(err)
	c.Assert(err, NotNil)
}

func (ts *HTestSuite) TestEnsureDir(c *C) {
	tempdir := c.MkDir()

	target := filepath.Join(tempdir, "meep")
	err := ensureDir(target, 0755)
	c.Assert(err, IsNil)
	st, err := os.Stat(target)
	c.Assert(err, IsNil)
	c.Assert(st.IsDir(), Equals, true)
	c.Assert(st.Mode(), Equals, os.ModeDir|0755)
}

func (ts *HTestSuite) TestMakeMapFromEnvList(c *C) {
	envList := []string{
		"PATH=/usr/bin:/bin",
		"DBUS_SESSION_BUS_ADDRESS=unix:abstract=something1234",
	}
	envMap := makeMapFromEnvList(envList)
	c.Assert(envMap, DeepEquals, map[string]string{
		"PATH": "/usr/bin:/bin",
		"DBUS_SESSION_BUS_ADDRESS": "unix:abstract=something1234",
	})
}

func (ts *HTestSuite) TestMakeMapFromEnvListInvalidInput(c *C) {
	envList := []string{
		"nonsesne",
	}
	envMap := makeMapFromEnvList(envList)
	c.Assert(envMap, DeepEquals, map[string]string(nil))
}

func (ts *HTestSuite) TestSha512sum(c *C) {
	tempdir := c.MkDir()

	p := filepath.Join(tempdir, "foo")
	err := ioutil.WriteFile(p, []byte("x"), 0644)
	c.Assert(err, IsNil)
	hashsum, err := sha512sum(p)
	c.Assert(err, IsNil)
	c.Assert(hashsum, Equals, "a4abd4448c49562d828115d13a1fccea927f52b4d5459297f8b43e42da89238bc13626e43dcb38ddb082488927ec904fb42057443983e88585179d50551afe62")
}

func (ts *HTestSuite) TestMakeConfigEnv(c *C) {
	tempdir := c.MkDir()
	yamlFile, err := makeMockSnap(tempdir)
	c.Assert(err, IsNil)
	snap := NewInstalledSnapPart(yamlFile)
	c.Assert(snap, NotNil)

	os.Setenv("SNAP_NAME", "override-me")
	defer os.Setenv("SNAP_NAME", "")

	env := makeSnapHookEnv(snap)

	// now ensure that the environment we get back is what we want
	envMap := makeMapFromEnvList(env)
	// regular env is unaltered
	c.Assert(envMap["PATH"], Equals, os.Getenv("PATH"))
	// SNAP_* is overriden
	c.Assert(envMap["SNAP_NAME"], Equals, "hello-app")
	c.Assert(envMap["SNAP_VERSION"], Equals, "1.10")
}

func (ts *HTestSuite) TestMakeRandomString(c *C) {
	// for our tests
	rand.Seed(1)

	s1 := makeRandomString(10)
	c.Assert(s1, Equals, "GMWjGsAPga")

	s2 := makeRandomString(5)
	c.Assert(s2, Equals, "TlmOD")
}

func (ts *HTestSuite) TestAtomicWriteFile(c *C) {
	tmpdir := c.MkDir()

	p := filepath.Join(tmpdir, "foo")
	err := atomicWriteFile(p, []byte("canary"), 0644)
	c.Assert(err, IsNil)

	content, err := ioutil.ReadFile(p)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, "canary")

	// no files left behind!
	d, err := ioutil.ReadDir(tmpdir)
	c.Assert(err, IsNil)
	c.Assert(len(d), Equals, 1)
}
