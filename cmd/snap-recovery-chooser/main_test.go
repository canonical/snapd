// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"io"
	"log/syslog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/client"
	main "github.com/snapcore/snapd/cmd/snap-recovery-chooser"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type baseCmdSuite struct {
	testutil.BaseTest

	stdout, stderr bytes.Buffer
	markerFile     string
}

func (s *baseCmdSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	_, r := logger.MockLogger()
	s.AddCleanup(r)
	r = main.MockStdStreams(&s.stdout, &s.stderr)
	s.AddCleanup(r)

	d := c.MkDir()
	s.markerFile = filepath.Join(d, "marker")
	mylog.Check(os.WriteFile(s.markerFile, nil, 0644))

}

type cmdSuite struct {
	baseCmdSuite
}

var _ = Suite(&cmdSuite{})

var mockSystems = &main.ChooserSystems{
	Systems: []client.System{
		{
			Label: "foo",
			Actions: []client.SystemAction{
				{Title: "reinstall", Mode: "install"},
			},
		},
	},
}

func (s *cmdSuite) TestRunUIHappy(c *C) {
	mockCmd := testutil.MockCommand(c, "tool", `
echo '{}'
`)
	defer mockCmd.Restore()

	rsp := mylog.Check2(main.RunUI(exec.Command(mockCmd.Exe()), mockSystems))

	c.Assert(rsp, NotNil)
}

func (s *cmdSuite) TestRunUIBadJSON(c *C) {
	mockCmd := testutil.MockCommand(c, "tool", `
echo 'garbage'
`)
	defer mockCmd.Restore()

	rsp := mylog.Check2(main.RunUI(exec.Command(mockCmd.Exe()), mockSystems))
	c.Assert(err, ErrorMatches, "cannot decode response: .*")
	c.Assert(rsp, IsNil)
}

func (s *cmdSuite) TestRunUIToolErr(c *C) {
	mockCmd := testutil.MockCommand(c, "tool", `
echo foo
exit 22
`)
	defer mockCmd.Restore()

	_ := mylog.Check2(main.RunUI(exec.Command(mockCmd.Exe()), mockSystems))
	c.Assert(err, ErrorMatches, "cannot collect output of the UI process: exit status 22")
}

func (s *cmdSuite) TestRunUIInputJSON(c *C) {
	d := c.MkDir()
	tf := filepath.Join(d, "json-input")
	mockCmd := testutil.MockCommand(c, "tool", fmt.Sprintf(`
cat > %s
echo '{}'
`, tf))
	defer mockCmd.Restore()

	_ := mylog.Check2(main.RunUI(exec.Command(mockCmd.Exe()), mockSystems))


	data := mylog.Check2(os.ReadFile(tf))

	var input *main.ChooserSystems
	mylog.Check(json.Unmarshal(data, &input))


	c.Assert(input, DeepEquals, mockSystems)
}

func (s *cmdSuite) TestStdoutUI(c *C) {
	var buf bytes.Buffer
	mylog.Check(main.OutputForUI(&buf, mockSystems))


	var out *main.ChooserSystems
	mylog.Check(json.Unmarshal(buf.Bytes(), &out))

	c.Assert(out, DeepEquals, mockSystems)
}

type mockedClientCmdSuite struct {
	baseCmdSuite

	config client.Config
}

var _ = Suite(&mockedClientCmdSuite{})

func (s *mockedClientCmdSuite) SetUpTest(c *C) {
	s.baseCmdSuite.SetUpTest(c)
}

func (s *mockedClientCmdSuite) RedirectClientToTestServer(handler func(http.ResponseWriter, *http.Request)) {
	server := httptest.NewServer(http.HandlerFunc(handler))
	s.BaseTest.AddCleanup(func() { server.Close() })
	s.config.BaseURL = server.URL
}

type mockSystemRequestResponse struct {
	label  string
	code   int
	reboot bool
	expect map[string]interface{}
}

func (s *mockedClientCmdSuite) mockSuccessfulResponse(c *C, rspSystems *main.ChooserSystems, rspPostSystem *mockSystemRequestResponse) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.URL.Path, Equals, "/v2/systems")
			mylog.Check(json.NewEncoder(w).Encode(apiResponse{
				Type:       "sync",
				Result:     rspSystems,
				StatusCode: 200,
			}))

		case 1:
			if rspPostSystem == nil {
				c.Fatalf("unexpected request to %q", r.URL.Path)
			}
			c.Check(r.URL.Path, Equals, "/v2/systems/"+rspPostSystem.label)
			c.Check(r.Method, Equals, "POST")

			var data map[string]interface{}
			mylog.Check(json.NewDecoder(r.Body).Decode(&data))

			c.Check(data, DeepEquals, rspPostSystem.expect)

			rspType := "sync"
			var rspData map[string]string
			if rspPostSystem.code >= 400 {
				rspType = "error"
				rspData = map[string]string{"message": "failed in mock"}
			}
			var maintenance map[string]interface{}
			if rspPostSystem.reboot {
				maintenance = map[string]interface{}{
					"kind":    client.ErrorKindSystemRestart,
					"message": "system is restartring",
				}
			}
			mylog.Check(json.NewEncoder(w).Encode(apiResponse{
				Type:        rspType,
				Result:      rspData,
				StatusCode:  rspPostSystem.code,
				Maintenance: maintenance,
			}))

		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}
		n++
	})
}

type apiResponse struct {
	Type        string      `json:"type"`
	Result      interface{} `json:"result"`
	StatusCode  int         `json:"status-code"`
	Maintenance interface{} `json:"maintenance"`
}

func (s *mockedClientCmdSuite) TestMainChooserWithTool(c *C) {
	r := main.MockDefaultMarkerFile(s.markerFile)
	defer r()
	// validity
	c.Assert(s.markerFile, testutil.FilePresent)

	capturedStdinPath := filepath.Join(c.MkDir(), "stdin")
	mockCmd := testutil.MockCommand(c, "tool", fmt.Sprintf(`
cat - > %s
echo '{"label":"label","action":{"mode":"install","title":"reinstall"}}'
`, capturedStdinPath))
	defer mockCmd.Restore()
	r = main.MockChooserTool(func() (*exec.Cmd, error) {
		return exec.Command(mockCmd.Exe()), nil
	})
	defer r()

	s.mockSuccessfulResponse(c, mockSystems, &mockSystemRequestResponse{
		code:  200,
		label: "label",
		expect: map[string]interface{}{
			"action": "do",
			"mode":   "install",
			"title":  "reinstall",
		},
		reboot: true,
	})

	rbt := mylog.Check2(main.Chooser(client.New(&s.config)))

	c.Assert(rbt, Equals, true)
	c.Assert(mockCmd.Calls(), DeepEquals, [][]string{
		{"tool"},
	})

	capturedStdin := mylog.Check2(os.ReadFile(capturedStdinPath))

	var stdoutSystems main.ChooserSystems
	mylog.Check(json.Unmarshal(capturedStdin, &stdoutSystems))

	c.Check(&stdoutSystems, DeepEquals, mockSystems)

	c.Assert(s.markerFile, testutil.FileAbsent)
}

func (s *mockedClientCmdSuite) TestMainChooserToolNotFound(c *C) {
	r := main.MockDefaultMarkerFile(s.markerFile)
	defer r()
	// validity
	c.Assert(s.markerFile, testutil.FilePresent)

	s.mockSuccessfulResponse(c, mockSystems, nil)

	r = main.MockChooserTool(func() (*exec.Cmd, error) {
		return nil, fmt.Errorf("tool not found")
	})
	defer r()

	rbt := mylog.Check2(main.Chooser(client.New(&s.config)))
	c.Assert(err, ErrorMatches, "cannot locate the chooser UI tool: tool not found")
	c.Assert(rbt, Equals, false)

	c.Assert(s.markerFile, testutil.FileAbsent)
}

func (s *mockedClientCmdSuite) TestMainChooserBadAPI(c *C) {
	r := main.MockDefaultMarkerFile(s.markerFile)
	defer r()
	// validity
	c.Assert(s.markerFile, testutil.FilePresent)

	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.URL.Path, Equals, "/v2/systems")
			enc := json.NewEncoder(w)
			mylog.Check(enc.Encode(apiResponse{
				Type: "error",
				Result: map[string]string{
					"message": "no systems for you",
				},
				StatusCode: 400,
			}))

		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}
		n++
	})

	rbt := mylog.Check2(main.Chooser(client.New(&s.config)))
	c.Assert(err, ErrorMatches, "cannot list recovery systems: no systems for you")
	c.Assert(rbt, Equals, false)

	c.Assert(s.markerFile, testutil.FileAbsent)
}

func (s *mockedClientCmdSuite) testMainChooserConsoleConfAlternatives(c *C, setupCmd func(script string) *testutil.MockCmd) {
	r := main.MockDefaultMarkerFile(s.markerFile)
	defer r()
	// validity
	c.Assert(s.markerFile, testutil.FilePresent)

	s.mockSuccessfulResponse(c, mockSystems, &mockSystemRequestResponse{
		code:  200,
		label: "label",
		expect: map[string]interface{}{
			"action": "do",
			"mode":   "install",
			"title":  "reinstall",
		},
	})

	mockCmd := setupCmd(`
echo '{"label":"label","action":{"mode":"install","title":"reinstall"}}'
`)

	defer mockCmd.Restore()

	rbt := mylog.Check2(main.Chooser(client.New(&s.config)))

	c.Assert(rbt, Equals, false)

	c.Check(mockCmd.Calls(), DeepEquals, [][]string{
		{"console-conf", "--recovery-chooser-mode"},
	})

	c.Assert(s.markerFile, testutil.FileAbsent)
}

func (s *mockedClientCmdSuite) TestMainChooserDefaultToConsoleConf(c *C) {
	d := c.MkDir()
	dirs.SetRootDir(d)
	defer dirs.SetRootDir("/")

	s.testMainChooserConsoleConfAlternatives(c, func(script string) *testutil.MockCmd {
		return testutil.MockCommand(c, filepath.Join(dirs.GlobalRootDir, "/usr/bin/console-conf"),
			script)
	})
}

func (s *mockedClientCmdSuite) TestMainChooserFallbackToSnapConsoleConf(c *C) {
	d := c.MkDir()
	dirs.SetRootDir(d)
	defer dirs.SetRootDir("/")

	s.testMainChooserConsoleConfAlternatives(c, func(script string) *testutil.MockCmd {
		// create /snap/bin/console-conf as a symlink like when a snap
		// is installed
		c.Assert(os.MkdirAll(dirs.SnapBinariesDir, 0755), IsNil)
		mylog.Check(os.Symlink(filepath.Join(d, "console-conf"),
			filepath.Join(dirs.SnapBinariesDir, "console-conf")))

		return testutil.MockCommand(c, filepath.Join(d, "console-conf"), script)
	})
}

func (s *mockedClientCmdSuite) TestMainChooserNoConsoleConf(c *C) {
	d := c.MkDir()
	dirs.SetRootDir(d)
	defer dirs.SetRootDir("/")

	r := main.MockDefaultMarkerFile(s.markerFile)
	defer r()
	// validity
	c.Assert(s.markerFile, testutil.FilePresent)

	// not expecting a POST request
	s.mockSuccessfulResponse(c, mockSystems, nil)

	// tries to look up the console-conf binary but fails
	rbt := mylog.Check2(main.Chooser(client.New(&s.config)))
	c.Assert(err, ErrorMatches, `cannot locate the chooser UI tool: chooser UI tools \[".*/usr/bin/console-conf" ".*snap/bin/console-conf"\] do not exist`)
	c.Assert(rbt, Equals, false)
	c.Assert(s.markerFile, testutil.FileAbsent)
}

func (s *mockedClientCmdSuite) TestMainChooserGarbageNoActionRequested(c *C) {
	d := c.MkDir()
	dirs.SetRootDir(d)
	defer dirs.SetRootDir("/")

	r := main.MockDefaultMarkerFile(s.markerFile)
	defer r()
	// validity
	c.Assert(s.markerFile, testutil.FilePresent)

	// not expecting a POST request
	s.mockSuccessfulResponse(c, mockSystems, nil)

	mockCmd := testutil.MockCommand(c, filepath.Join(dirs.GlobalRootDir, "/usr/bin/console-conf"), `
echo 'garbage'
`)
	defer mockCmd.Restore()

	rbt := mylog.Check2(main.Chooser(client.New(&s.config)))
	c.Assert(err, ErrorMatches, "UI process failed: cannot decode response: .*")
	c.Assert(rbt, Equals, false)

	c.Check(mockCmd.Calls(), DeepEquals, [][]string{
		{"console-conf", "--recovery-chooser-mode"},
	})

	c.Assert(s.markerFile, testutil.FileAbsent)
}

func (s *mockedClientCmdSuite) TestMainChooserNoMarkerNoCalls(c *C) {
	r := main.MockDefaultMarkerFile(s.markerFile + ".notfound")
	defer r()

	mockCmd := testutil.MockCommand(c, "tool", `
exit 123
`)
	defer mockCmd.Restore()
	r = main.MockChooserTool(func() (*exec.Cmd, error) {
		return exec.Command(mockCmd.Exe()), nil
	})
	defer r()

	rbt := mylog.Check2(main.Chooser(client.New(&s.config)))
	c.Assert(err, ErrorMatches, "cannot run chooser without the marker file")
	c.Assert(rbt, Equals, false)

	c.Assert(mockCmd.Calls(), HasLen, 0)
}

func (s *mockedClientCmdSuite) TestMainChooserSnapdAPIBad(c *C) {
	r := main.MockDefaultMarkerFile(s.markerFile)
	defer r()
	// validity
	c.Assert(s.markerFile, testutil.FilePresent)

	mockCmd := testutil.MockCommand(c, "tool", `
echo '{"label":"label","action":{"mode":"install","title":"reinstall"}}'
`)
	defer mockCmd.Restore()
	r = main.MockChooserTool(func() (*exec.Cmd, error) {
		return exec.Command(mockCmd.Exe()), nil
	})
	defer r()

	s.mockSuccessfulResponse(c, mockSystems, &mockSystemRequestResponse{
		code:  400,
		label: "label",
		expect: map[string]interface{}{
			"action": "do",
			"mode":   "install",
			"title":  "reinstall",
		},
	})

	rbt := mylog.Check2(main.Chooser(client.New(&s.config)))
	c.Assert(err, ErrorMatches, "cannot request system action: .* failed in mock")
	c.Assert(rbt, Equals, false)
	c.Assert(mockCmd.Calls(), DeepEquals, [][]string{
		{"tool"},
	})

	c.Assert(s.markerFile, testutil.FileAbsent)
}

type mockedSyslogCmdSuite struct {
	baseCmdSuite

	term string
}

var _ = Suite(&mockedSyslogCmdSuite{})

func (s *mockedSyslogCmdSuite) SetUpTest(c *C) {
	s.baseCmdSuite.SetUpTest(c)

	s.term = os.Getenv("TERM")
	s.AddCleanup(func() { os.Setenv("TERM", s.term) })

	r := main.MockSyslogNew(func(p syslog.Priority, t string) (io.Writer, error) {
		c.Fatal("not mocked")
		return nil, fmt.Errorf("not mocked")
	})
	s.AddCleanup(r)
}

func (s *mockedSyslogCmdSuite) TestNoSyslogFallback(c *C) {
	mylog.Check(os.Setenv("TERM", "someterm"))


	called := false
	r := main.MockSyslogNew(func(_ syslog.Priority, _ string) (io.Writer, error) {
		called = true
		return nil, fmt.Errorf("no syslog")
	})
	defer r()
	mylog.Check(main.LoggerWithSyslogMaybe())

	c.Check(called, Equals, true)
	// this likely goes to stderr
	logger.Noticef("ping")
}

func (s *mockedSyslogCmdSuite) TestWithSyslog(c *C) {
	mylog.Check(os.Setenv("TERM", "someterm"))


	called := false
	tag := ""
	prio := syslog.Priority(0)
	buf := bytes.Buffer{}
	r := main.MockSyslogNew(func(p syslog.Priority, tg string) (io.Writer, error) {
		tag = tg
		prio = p
		called = true
		return &buf, nil
	})
	defer r()
	mylog.Check(main.LoggerWithSyslogMaybe())

	c.Check(called, Equals, true)
	c.Check(tag, Equals, "snap-recovery-chooser")
	c.Check(prio, Equals, syslog.LOG_INFO|syslog.LOG_DAEMON)

	logger.Noticef("ping")
	c.Check(buf.String(), testutil.Contains, "ping")
}

func (s *mockedSyslogCmdSuite) TestSimple(c *C) {
	mylog.Check(os.Unsetenv("TERM"))


	r := main.MockSyslogNew(func(p syslog.Priority, tg string) (io.Writer, error) {
		c.Fatalf("unexpected call")
		return nil, fmt.Errorf("unexpected call")
	})
	defer r()
	mylog.Check(main.LoggerWithSyslogMaybe())

}
