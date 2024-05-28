// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2024 Canonical Ltd
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

package main_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jessevdk/go-flags"
	"golang.org/x/crypto/ssh/terminal"
	. "gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/kcmdline"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/snapdtool"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type BaseSnapSuite struct {
	testutil.BaseTest
	stdin    *bytes.Buffer
	stdout   *bytes.Buffer
	stderr   *bytes.Buffer
	password string

	AuthFile string
}

func (s *BaseSnapSuite) readPassword(fd int) ([]byte, error) {
	return []byte(s.password), nil
}

func (s *BaseSnapSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())

	path := os.Getenv("PATH")
	s.AddCleanup(func() {
		os.Setenv("PATH", path)
	})
	os.Setenv("PATH", path+":"+dirs.SnapBinariesDir)

	s.stdin = bytes.NewBuffer(nil)
	s.stdout = bytes.NewBuffer(nil)
	s.stderr = bytes.NewBuffer(nil)
	s.password = ""

	snap.Stdin = s.stdin
	snap.Stdout = s.stdout
	snap.Stderr = s.stderr
	snap.ReadPassword = s.readPassword
	s.AuthFile = filepath.Join(c.MkDir(), "json")
	os.Setenv(TestAuthFileEnvKey, s.AuthFile)

	s.AddCleanup(interfaces.MockSystemKey(`
{
"build-id": "7a94e9736c091b3984bd63f5aebfc883c4d859e0",
"apparmor-features": ["caps", "dbus"]
}`))
	err := os.MkdirAll(filepath.Dir(dirs.SnapSystemKeyFile), 0755)
	c.Assert(err, IsNil)
	extraData := interfaces.SystemKeyExtraData{}
	err = interfaces.WriteSystemKey(extraData)
	c.Assert(err, IsNil)

	s.AddCleanup(snap.MockIsStdoutTTY(false))
	s.AddCleanup(snap.MockIsStdinTTY(false))

	s.AddCleanup(snap.MockSELinuxIsEnabled(func() (bool, error) { return false, nil }))

	// mock an empty cmdline since we check the cmdline to check whether we are
	// in install mode or not and we don't want to use the host's proc/cmdline
	s.AddCleanup(kcmdline.MockProcCmdline(filepath.Join(c.MkDir(), "proc/cmdline")))
}

func (s *BaseSnapSuite) TearDownTest(c *C) {
	snap.Stdin = os.Stdin
	snap.Stdout = os.Stdout
	snap.Stderr = os.Stderr
	snap.ReadPassword = terminal.ReadPassword

	c.Assert(s.AuthFile == "", Equals, false)
	err := os.Unsetenv(TestAuthFileEnvKey)
	c.Assert(err, IsNil)
	dirs.SetRootDir("/")
	s.BaseTest.TearDownTest(c)
}

func (s *BaseSnapSuite) Stdout() string {
	return s.stdout.String()
}

func (s *BaseSnapSuite) Stderr() string {
	return s.stderr.String()
}

func (s *BaseSnapSuite) ResetStdStreams() {
	s.stdin.Reset()
	s.stdout.Reset()
	s.stderr.Reset()
}

func (s *BaseSnapSuite) RedirectClientToTestServer(handler func(http.ResponseWriter, *http.Request)) {
	server := httptest.NewServer(http.HandlerFunc(handler))
	s.BaseTest.AddCleanup(func() { server.Close() })
	snap.ClientConfig.BaseURL = server.URL
	s.BaseTest.AddCleanup(func() { snap.ClientConfig.BaseURL = "" })
}

func (s *BaseSnapSuite) Login(c *C) {
	err := osutil.AtomicWriteFile(s.AuthFile, []byte(TestAuthFileContents), 0600, 0)
	c.Assert(err, IsNil)
}

func (s *BaseSnapSuite) Logout(c *C) {
	if osutil.FileExists(s.AuthFile) {
		c.Assert(os.Remove(s.AuthFile), IsNil)
	}
}

type SnapSuite struct {
	BaseSnapSuite
}

var _ = Suite(&SnapSuite{})

// DecodedRequestBody returns the JSON-decoded body of the request.
func DecodedRequestBody(c *C, r *http.Request) map[string]interface{} {
	var body map[string]interface{}
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	err := decoder.Decode(&body)
	c.Assert(err, IsNil)
	return body
}

// EncodeResponseBody writes JSON-serialized body to the response writer.
func EncodeResponseBody(c *C, w http.ResponseWriter, body interface{}) {
	encoder := json.NewEncoder(w)
	err := encoder.Encode(body)
	c.Assert(err, IsNil)
}

func mockArgs(args ...string) (restore func()) {
	old := os.Args
	os.Args = args
	return func() { os.Args = old }
}

func mockSnapConfine(libExecDir string) func() {
	snapConfine := filepath.Join(libExecDir, "snap-confine")
	if err := os.MkdirAll(libExecDir, 0755); err != nil {
		panic(err)
	}
	if err := os.WriteFile(snapConfine, nil, 0644); err != nil {
		panic(err)
	}
	return func() {
		if err := os.Remove(snapConfine); err != nil {
			panic(err)
		}
	}
}

const TestAuthFileEnvKey = "SNAPD_AUTH_DATA_FILENAME"
const TestAuthFileContents = `{"id":123,"email":"hello@mail.com","macaroon":"MDAxM2xvY2F0aW9uIHNuYXBkCjAwMTJpZGVudGlmaWVyIDQzCjAwMmZzaWduYXR1cmUg5RfMua72uYop4t3cPOBmGUuaoRmoDH1HV62nMJq7eqAK"}`

func (s *SnapSuite) TestErrorResult(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "cannot do something"}}`)
	})

	restore := mockArgs("snap", "install", "foo")
	defer restore()

	err := snap.RunMain()
	c.Assert(err, ErrorMatches, `cannot do something`)
}

func (s *SnapSuite) TestAccessDeniedHint(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "access denied", "kind": "login-required"}, "status-code": 401}`)
	})

	restore := mockArgs("snap", "install", "foo")
	defer restore()

	err := snap.RunMain()
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, `access denied (try with sudo)`)
}

func (s *SnapSuite) TestExtraArgs(c *C) {
	restore := mockArgs("snap", "abort", "1", "xxx", "zzz")
	defer restore()

	err := snap.RunMain()
	c.Assert(err, ErrorMatches, `too many arguments for command`)
}

func (s *SnapSuite) TestVersionOnClassic(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type":"sync","status-code":200,"status":"OK","result":{"on-classic":true,"os-release":{"id":"ubuntu","version-id":"12.34"},"series":"56","version":"7.89"}}`)
	})
	restore := mockArgs("snap", "--version")
	defer restore()
	restore = snapdtool.MockVersion("4.56")
	defer restore()

	c.Assert(func() { snap.RunMain() }, PanicMatches, `internal error: exitStatus\{0\} .*`)
	c.Assert(s.Stdout(), Equals, "snap    4.56\nsnapd   7.89\nseries  56\nubuntu  12.34\n")
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestVersionOnAllSnap(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type":"sync","status-code":200,"status":"OK","result":{"os-release":{"id":"ubuntu","version-id":"12.34"},"series":"56","version":"7.89"}}`)
	})
	restore := mockArgs("snap", "--version")
	defer restore()
	restore = snapdtool.MockVersion("4.56")
	defer restore()

	c.Assert(func() { snap.RunMain() }, PanicMatches, `internal error: exitStatus\{0\} .*`)
	c.Assert(s.Stdout(), Equals, "snap    4.56\nsnapd   7.89\nseries  56\n")
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestUnknownCommand(c *C) {
	restore := mockArgs("snap", "unknowncmd")
	defer restore()

	err := snap.RunMain()
	c.Assert(err, ErrorMatches, `unknown command "unknowncmd", see 'snap help'.`)
}

func (s *SnapSuite) TestNoCommandWithArgs(c *C) {
	for _, args := range [][]string{
		{"snap", "--foo"},
		{"snap", "--bar", "install"},
		{"snap", "-f"},
		{"snap", "-b", "refresh"},
	} {
		restore := mockArgs(args...)
		err := snap.RunMain()

		flag := strings.TrimLeft(args[1], "-")
		c.Assert(err, ErrorMatches, fmt.Sprintf("unknown flag `%s'", flag))
		restore()
	}
}

func (s *SnapSuite) TestResolveApp(c *C) {
	err := os.MkdirAll(dirs.SnapBinariesDir, 0755)
	c.Assert(err, IsNil)

	// "wrapper" symlinks
	err = os.Symlink("/usr/bin/snap", filepath.Join(dirs.SnapBinariesDir, "foo"))
	c.Assert(err, IsNil)
	err = os.Symlink("/usr/bin/snap", filepath.Join(dirs.SnapBinariesDir, "foo.bar"))
	c.Assert(err, IsNil)

	// alias symlinks
	err = os.Symlink("foo", filepath.Join(dirs.SnapBinariesDir, "foo_"))
	c.Assert(err, IsNil)
	err = os.Symlink("foo.bar", filepath.Join(dirs.SnapBinariesDir, "foo_bar-1"))
	c.Assert(err, IsNil)

	snapApp, err := snap.ResolveApp("foo")
	c.Assert(err, IsNil)
	c.Check(snapApp, Equals, "foo")

	snapApp, err = snap.ResolveApp("foo.bar")
	c.Assert(err, IsNil)
	c.Check(snapApp, Equals, "foo.bar")

	snapApp, err = snap.ResolveApp("foo_")
	c.Assert(err, IsNil)
	c.Check(snapApp, Equals, "foo")

	snapApp, err = snap.ResolveApp("foo_bar-1")
	c.Assert(err, IsNil)
	c.Check(snapApp, Equals, "foo.bar")

	_, err = snap.ResolveApp("baz")
	c.Check(err, NotNil)
}

func (s *SnapSuite) TestFirstNonOptionIsRun(c *C) {
	osArgs := os.Args
	defer func() {
		os.Args = osArgs
	}()
	for _, negative := range []string{
		"",
		"snap",
		"snap verb",
		"snap verb --flag arg",
		"snap verb arg --flag",
		"snap --global verb --flag arg",
	} {
		os.Args = strings.Fields(negative)
		c.Check(snap.FirstNonOptionIsRun(), Equals, false)
	}

	for _, positive := range []string{
		"snap run",
		"snap run --flag",
		"snap run --flag arg",
		"snap run arg --flag",
		"snap --global run",
		"snap --global run --flag",
		"snap --global run --flag arg",
		"snap --global run arg --flag",
	} {
		os.Args = strings.Fields(positive)
		c.Check(snap.FirstNonOptionIsRun(), Equals, true)
	}
}

func (s *SnapSuite) TestLintDesc(c *C) {
	log, restore := logger.MockLogger()
	defer restore()

	// LintDesc doesn't panic or log if SNAPD_DEBUG or testing are unset
	snap.LintDesc("command", "<option>", "description", "")
	c.Check(log.String(), HasLen, 0)

	restoreTesting := snapdenv.MockTesting(true)
	defer restoreTesting()

	// LintDesc is happy about capitalized description.
	snap.LintDesc("command", "<option>", "Description ...", "")
	c.Check(log.String(), HasLen, 0)
	log.Reset()

	// LintDesc complains about lowercase description and mentions the locale
	// that the system is currently in.
	prevValue := os.Getenv("LC_MESSAGES")
	os.Setenv("LC_MESSAGES", "en_US")
	defer func() {
		os.Setenv("LC_MESSAGES", prevValue)
	}()

	fn := func() {
		snap.LintDesc("command", "<option>", "description", "")
	}
	c.Check(fn, PanicMatches, `description of command's "<option>" is lowercase in locale "en_US": "description"`)
	log.Reset()

	// LintDesc does not complain about lowercase description starting with login.ubuntu.com
	snap.LintDesc("command", "<option>", "login.ubuntu.com description", "")
	c.Check(log.String(), HasLen, 0)
	log.Reset()

	// LintDesc panics when original description is present.
	fn = func() {
		snap.LintDesc("command", "<option>", "description", "original description")
	}
	c.Check(fn, PanicMatches, `description of command's "<option>" of "original description" set from tag \(=> no i18n\)`)
	log.Reset()

	// LintDesc panics when option name is empty.
	fn = func() {
		snap.LintDesc("command", "", "description", "")
	}
	c.Check(fn, PanicMatches, `option on "command" has no name`)
	log.Reset()

	snap.LintDesc("snap-advise", "from-apt", "snap-advise will run as a hook", "")
	c.Check(log.String(), HasLen, 0)
	log.Reset()
}

func (s *SnapSuite) TestLintArg(c *C) {
	log, restore := logger.MockLogger()
	defer restore()

	// LintArg doesn't panic or log if SNAPD_DEBUG or testing are unset
	snap.LintArg("command", "option", "Description", "")
	c.Check(log.String(), HasLen, 0)

	restoreTest := snapdenv.MockTesting(true)
	defer restoreTest()

	// LintArg is happy when option is enclosed with < >.
	log.Reset()
	snap.LintArg("command", "<option>", "Description", "")
	c.Check(log.String(), HasLen, 0)

	// LintArg complains about when option is not properly enclosed with < >.
	fn := func() {
		snap.LintArg("command", "option", "Description", "")
	}
	c.Check(fn, PanicMatches, `argument "command"'s "option" should begin with < and end with >`)

	fn = func() {
		snap.LintArg("command", "<option", "Description", "")
	}
	c.Check(fn, PanicMatches, `argument "command"'s "<option" should begin with < and end with >`)

	fn = func() {
		snap.LintArg("command", "option>", "Description", "")
	}
	c.Check(fn, PanicMatches, `argument "command"'s "option>" should begin with < and end with >`)

	// LintArg ignores the special case of <option>s.
	log.Reset()
	snap.LintArg("command", "<option>s", "Description", "")
	c.Check(log.String(), HasLen, 0)
}

func (s *SnapSuite) TestFixupArg(c *C) {
	c.Check(snap.FixupArg("option"), Equals, "option")
	c.Check(snap.FixupArg("<option>"), Equals, "<option>")
	// Trailing ">s" is fixed to just >.
	c.Check(snap.FixupArg("<option>s"), Equals, "<option>")
}

func (s *SnapSuite) TestSetsUserAgent(c *C) {
	testServerHit := false
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("User-Agent"), Matches, "snapd/.*")
		testServerHit = true

		fmt.Fprintln(w, `{"type": "error", "result": {"message": "cannot do something"}}`)
	})
	restore := mockArgs("snap", "install", "foo")
	defer restore()

	_ = snap.RunMain()
	c.Assert(testServerHit, Equals, true)
}

func (s *SnapSuite) TestCompletionHandlerSkipsHidden(c *C) {
	snap.MarkForNoCompletion(snap.HiddenCmd("bar yadda yack", false))
	snap.MarkForNoCompletion(snap.HiddenCmd("bar yack yack yack", true))
	snap.CompletionHandler([]flags.Completion{
		{Item: "foo", Description: "foo yadda yadda"},
		{Item: "bar", Description: "bar yadda yack"},
		{Item: "baz", Description: "bar yack yack yack"},
	})
	c.Check(s.Stdout(), Equals, "foo\nbaz\n")
}
