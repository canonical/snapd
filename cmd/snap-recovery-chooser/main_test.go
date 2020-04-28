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
	"io/ioutil"
	"log/syslog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

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
	err := ioutil.WriteFile(s.markerFile, nil, 0644)
	c.Assert(err, IsNil)
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

	rsp, err := main.RunUI(exec.Command(mockCmd.Exe()), mockSystems)
	c.Assert(err, IsNil)
	c.Assert(rsp, NotNil)
}

func (s *cmdSuite) TestRunUIBadJSON(c *C) {
	mockCmd := testutil.MockCommand(c, "tool", `
echo 'garbage'
`)
	defer mockCmd.Restore()

	rsp, err := main.RunUI(exec.Command(mockCmd.Exe()), mockSystems)
	c.Assert(err, ErrorMatches, "cannot decode response: .*")
	c.Assert(rsp, IsNil)
}

func (s *cmdSuite) TestRunUIToolErr(c *C) {
	mockCmd := testutil.MockCommand(c, "tool", `
echo foo
exit 22
`)
	defer mockCmd.Restore()

	_, err := main.RunUI(exec.Command(mockCmd.Exe()), mockSystems)
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

	_, err := main.RunUI(exec.Command(mockCmd.Exe()), mockSystems)
	c.Assert(err, IsNil)

	data, err := ioutil.ReadFile(tf)
	c.Assert(err, IsNil)
	var input *main.ChooserSystems
	err = json.Unmarshal(data, &input)
	c.Assert(err, IsNil)

	c.Assert(input, DeepEquals, mockSystems)
}

func (s *cmdSuite) TestStdoutUI(c *C) {
	var buf bytes.Buffer
	err := main.OutputForUI(&buf, mockSystems)
	c.Assert(err, IsNil)

	var out *main.ChooserSystems

	err = json.Unmarshal(buf.Bytes(), &out)
	c.Assert(err, IsNil)
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
			err := json.NewEncoder(w).Encode(apiResponse{
				Type:       "sync",
				Result:     rspSystems,
				StatusCode: 200,
			})
			c.Assert(err, IsNil)
		case 1:
			if rspPostSystem == nil {
				c.Fatalf("unexpected request to %q", r.URL.Path)
			}
			c.Check(r.URL.Path, Equals, "/v2/systems/"+rspPostSystem.label)
			c.Check(r.Method, Equals, "POST")

			var data map[string]interface{}
			err := json.NewDecoder(r.Body).Decode(&data)
			c.Assert(err, IsNil)
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
			err = json.NewEncoder(w).Encode(apiResponse{
				Type:        rspType,
				Result:      rspData,
				StatusCode:  rspPostSystem.code,
				Maintenance: maintenance,
			})
			c.Assert(err, IsNil)
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
	// sanity
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

	rbt, err := main.Chooser(client.New(&s.config))
	c.Assert(err, IsNil)
	c.Assert(rbt, Equals, true)
	c.Assert(mockCmd.Calls(), DeepEquals, [][]string{
		{"tool"},
	})

	capturedStdin, err := ioutil.ReadFile(capturedStdinPath)
	c.Assert(err, IsNil)
	var stdoutSystems main.ChooserSystems
	err = json.Unmarshal(capturedStdin, &stdoutSystems)
	c.Assert(err, IsNil)
	c.Check(&stdoutSystems, DeepEquals, mockSystems)

	c.Assert(s.markerFile, testutil.FileAbsent)
}

func (s *mockedClientCmdSuite) TestMainChooserToolNotFound(c *C) {
	r := main.MockDefaultMarkerFile(s.markerFile)
	defer r()
	// sanity
	c.Assert(s.markerFile, testutil.FilePresent)

	s.mockSuccessfulResponse(c, mockSystems, nil)

	r = main.MockChooserTool(func() (*exec.Cmd, error) {
		return nil, fmt.Errorf("tool not found")
	})
	defer r()

	rbt, err := main.Chooser(client.New(&s.config))
	c.Assert(err, ErrorMatches, "cannot locate the chooser UI tool: tool not found")
	c.Assert(rbt, Equals, false)

	c.Assert(s.markerFile, testutil.FileAbsent)
}

func (s *mockedClientCmdSuite) TestMainChooserBadAPI(c *C) {
	r := main.MockDefaultMarkerFile(s.markerFile)
	defer r()
	// sanity
	c.Assert(s.markerFile, testutil.FilePresent)

	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.URL.Path, Equals, "/v2/systems")
			enc := json.NewEncoder(w)
			err := enc.Encode(apiResponse{
				Type: "error",
				Result: map[string]string{
					"message": "no systems for you",
				},
				StatusCode: 400,
			})
			c.Assert(err, IsNil)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}
		n++
	})

	rbt, err := main.Chooser(client.New(&s.config))
	c.Assert(err, ErrorMatches, "cannot list recovery systems: no systems for you")
	c.Assert(rbt, Equals, false)

	c.Assert(s.markerFile, testutil.FileAbsent)
}

func (s *mockedClientCmdSuite) TestMainChooserDefaultsToConsoleConf(c *C) {
	d := c.MkDir()
	dirs.SetRootDir(d)
	defer dirs.SetRootDir("/")

	r := main.MockDefaultMarkerFile(s.markerFile)
	defer r()
	// sanity
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

	mockCmd := testutil.MockCommand(c, filepath.Join(dirs.GlobalRootDir, "/usr/bin/console-conf"), `
echo '{"label":"label","action":{"mode":"install","title":"reinstall"}}'
`)
	defer mockCmd.Restore()

	rbt, err := main.Chooser(client.New(&s.config))
	c.Assert(err, IsNil)
	c.Assert(rbt, Equals, false)

	c.Check(mockCmd.Calls(), DeepEquals, [][]string{
		{"console-conf", "--recovery-chooser-mode"},
	})

	c.Assert(s.markerFile, testutil.FileAbsent)
}

func (s *mockedClientCmdSuite) TestMainChooserNoConsoleConf(c *C) {
	d := c.MkDir()
	dirs.SetRootDir(d)
	defer dirs.SetRootDir("/")

	r := main.MockDefaultMarkerFile(s.markerFile)
	defer r()
	// sanity
	c.Assert(s.markerFile, testutil.FilePresent)

	// not expecting a POST request
	s.mockSuccessfulResponse(c, mockSystems, nil)

	// tries to look up the console-conf binary but fails
	rbt, err := main.Chooser(client.New(&s.config))
	c.Assert(err, ErrorMatches, `cannot locate the chooser UI tool: chooser UI tool ".*/usr/bin/console-conf" does not exist`)
	c.Assert(rbt, Equals, false)
	c.Assert(s.markerFile, testutil.FileAbsent)
}

func (s *mockedClientCmdSuite) TestMainChooserGarbageNoActionRequested(c *C) {
	d := c.MkDir()
	dirs.SetRootDir(d)
	defer dirs.SetRootDir("/")

	r := main.MockDefaultMarkerFile(s.markerFile)
	defer r()
	// sanity
	c.Assert(s.markerFile, testutil.FilePresent)

	// not expecting a POST request
	s.mockSuccessfulResponse(c, mockSystems, nil)

	mockCmd := testutil.MockCommand(c, filepath.Join(dirs.GlobalRootDir, "/usr/bin/console-conf"), `
echo 'garbage'
`)
	defer mockCmd.Restore()

	rbt, err := main.Chooser(client.New(&s.config))
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

	rbt, err := main.Chooser(client.New(&s.config))
	c.Assert(err, ErrorMatches, "cannot run chooser without the marker file")
	c.Assert(rbt, Equals, false)

	c.Assert(mockCmd.Calls(), HasLen, 0)
}

func (s *mockedClientCmdSuite) TestMainChooserSnapdAPIBad(c *C) {
	r := main.MockDefaultMarkerFile(s.markerFile)
	defer r()
	// sanity
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

	rbt, err := main.Chooser(client.New(&s.config))
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
	err := os.Setenv("TERM", "someterm")
	c.Assert(err, IsNil)

	called := false
	r := main.MockSyslogNew(func(_ syslog.Priority, _ string) (io.Writer, error) {
		called = true
		return nil, fmt.Errorf("no syslog")
	})
	defer r()
	err = main.LoggerWithSyslogMaybe()
	c.Assert(err, IsNil)
	c.Check(called, Equals, true)
	// this likely goes to stderr
	logger.Noticef("ping")
}

func (s *mockedSyslogCmdSuite) TestWithSyslog(c *C) {
	err := os.Setenv("TERM", "someterm")
	c.Assert(err, IsNil)

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
	err = main.LoggerWithSyslogMaybe()
	c.Assert(err, IsNil)
	c.Check(called, Equals, true)
	c.Check(tag, Equals, "snap-recovery-chooser")
	c.Check(prio, Equals, syslog.LOG_INFO|syslog.LOG_DAEMON)

	logger.Noticef("ping")
	c.Check(buf.String(), testutil.Contains, "ping")
}

func (s *mockedSyslogCmdSuite) TestSimple(c *C) {
	err := os.Unsetenv("TERM")
	c.Assert(err, IsNil)

	r := main.MockSyslogNew(func(p syslog.Priority, tg string) (io.Writer, error) {
		c.Fatalf("unexpected call")
		return nil, fmt.Errorf("unexpected call")
	})
	defer r()
	err = main.LoggerWithSyslogMaybe()
	c.Assert(err, IsNil)
}
