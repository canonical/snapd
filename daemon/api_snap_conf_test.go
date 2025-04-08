// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2020 Canonical Ltd
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

package daemon_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/testutil"
)

var _ = check.Suite(&snapConfSuite{})

type snapConfSuite struct {
	apiBaseSuite
}

func (s *snapConfSuite) SetUpTest(c *check.C) {
	s.apiBaseSuite.SetUpTest(c)

	s.expectReadAccess(daemon.AuthenticatedAccess{Polkit: "io.snapcraft.snapd.manage-configuration"})
	s.expectWriteAccess(daemon.AuthenticatedAccess{Polkit: "io.snapcraft.snapd.manage-configuration"})

	// Skip fetching external configs in testing
	config.ClearExternalConfigMap()
}

func (s *snapConfSuite) runGetConf(c *check.C, snapName string, keys []string, statusCode int) map[string]any {
	req, err := http.NewRequest("GET", "/v2/snaps/"+snapName+"/conf?keys="+strings.Join(keys, ","), nil)
	c.Check(err, check.IsNil)
	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, statusCode)

	var body map[string]any
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	return body["result"].(map[string]any)
}

func (s *snapConfSuite) TestGetConfSingleKey(c *check.C) {
	d := s.daemon(c)

	// Set a config that we'll get in a moment
	d.Overlord().State().Lock()
	tr := config.NewTransaction(d.Overlord().State())
	tr.Set("test-snap", "test-key1", "test-value1")
	tr.Set("test-snap", "test-key2", "test-value2")
	tr.Commit()
	d.Overlord().State().Unlock()

	result := s.runGetConf(c, "test-snap", []string{"test-key1"}, 200)
	c.Check(result, check.DeepEquals, map[string]any{"test-key1": "test-value1"})

	result = s.runGetConf(c, "test-snap", []string{"test-key1", "test-key2"}, 200)
	c.Check(result, check.DeepEquals, map[string]any{"test-key1": "test-value1", "test-key2": "test-value2"})
}

func (s *snapConfSuite) TestGetConfCoreSystemAlias(c *check.C) {
	d := s.daemon(c)

	// Set a config that we'll get in a moment
	d.Overlord().State().Lock()
	tr := config.NewTransaction(d.Overlord().State())
	tr.Set("core", "test-key1", "test-value1")
	tr.Commit()
	d.Overlord().State().Unlock()

	result := s.runGetConf(c, "core", []string{"test-key1"}, 200)
	c.Check(result, check.DeepEquals, map[string]any{"test-key1": "test-value1"})

	result = s.runGetConf(c, "system", []string{"test-key1"}, 200)
	c.Check(result, check.DeepEquals, map[string]any{"test-key1": "test-value1"})
}

func (s *snapConfSuite) TestGetConfMissingKey(c *check.C) {
	s.daemon(c)
	result := s.runGetConf(c, "test-snap", []string{"test-key2"}, 400)
	c.Check(result, check.DeepEquals, map[string]any{
		"value": map[string]any{
			"SnapName": "test-snap",
			"Key":      "test-key2",
		},
		"message": `snap "test-snap" has no "test-key2" configuration option`,
		"kind":    "option-not-found",
	})
}

func (s *snapConfSuite) TestGetConfCoreUnsupportedExperimentalFlag(c *check.C) {
	d := s.daemon(c)

	// We are testing that experimental features that are no longer experimental
	// are hidden

	d.Overlord().State().Lock()
	tr := config.NewTransaction(d.Overlord().State())
	err := tr.Set("core", "experimental.old-flag", true)
	c.Assert(err, check.IsNil)
	err = tr.Set("core", "experimental.supported-flag", true)
	c.Assert(err, check.IsNil)
	tr.Commit()
	d.Overlord().State().Unlock()

	// Simulate that experimental.old-flag is now out of experimental
	restore := configcore.MockSupportedExperimentalFlags([]string{"supported-flag"})
	defer restore()

	// Exact query to old experimental feature are not pruned
	result := s.runGetConf(c, "core", []string{"experimental.old-flag"}, 200)
	c.Check(result, check.DeepEquals, map[string]any{
		"experimental.old-flag": true,
	})

	// Generic experimental features query should hide old experimental
	// features that are no longer supported
	result = s.runGetConf(c, "core", []string{"experimental"}, 200)
	c.Check(result, check.DeepEquals, map[string]any{
		"experimental": map[string]any{
			"supported-flag": true,
		},
	})
	// Let's only check experimental config in root document
	result = s.runGetConf(c, "core", nil, 200)
	result = result["experimental"].(map[string]any)
	c.Check(result, check.DeepEquals, map[string]any{"supported-flag": true})

	// Simulate the scenario where snapd is reverted to revision
	// that supports a hidden experimental feature
	restore = configcore.MockSupportedExperimentalFlags([]string{"supported-flag", "old-flag"})
	defer restore()

	// Exact queries are still shown
	result = s.runGetConf(c, "core", []string{"experimental.old-flag"}, 200)
	c.Check(result, check.DeepEquals, map[string]any{"experimental.old-flag": true})

	// Generic queries should now show previously hidden experimental feature
	result = s.runGetConf(c, "core", []string{"experimental"}, 200)
	c.Check(result, check.DeepEquals, map[string]any{
		"experimental": map[string]any{
			"old-flag":       true,
			"supported-flag": true,
		},
	})
	result = s.runGetConf(c, "core", nil, 200)
	result = result["experimental"].(map[string]any)
	c.Check(result, check.DeepEquals, map[string]any{"old-flag": true, "supported-flag": true})
}

func (s *snapConfSuite) TestGetRootDocument(c *check.C) {
	d := s.daemon(c)
	d.Overlord().State().Lock()
	tr := config.NewTransaction(d.Overlord().State())
	tr.Set("test-snap", "test-key1", "test-value1")
	tr.Set("test-snap", "test-key2", "test-value2")
	tr.Commit()
	d.Overlord().State().Unlock()

	result := s.runGetConf(c, "test-snap", nil, 200)
	c.Check(result, check.DeepEquals, map[string]any{"test-key1": "test-value1", "test-key2": "test-value2"})
}

func (s *snapConfSuite) TestGetConfBadKey(c *check.C) {
	s.daemon(c)
	// TODO: this one in particular should really be a 400 also
	result := s.runGetConf(c, "test-snap", []string{"."}, 500)
	c.Check(result, check.DeepEquals, map[string]any{"message": `invalid option name: ""`})
}

const configYaml = `
name: config-snap
version: 1
hooks:
    configure:
`

func (s *snapConfSuite) TestSetConf(c *check.C) {
	d := s.daemon(c)
	s.mockSnap(c, configYaml)

	// Mock the hook runner
	hookRunner := testutil.MockCommand(c, "snap", "")
	defer hookRunner.Restore()

	d.Overlord().Loop()
	defer d.Overlord().Stop()

	text, err := json.Marshal(map[string]any{"key": "value"})
	c.Assert(err, check.IsNil)

	buffer := bytes.NewBuffer(text)
	req, err := http.NewRequest("PUT", "/v2/snaps/config-snap/conf", buffer)
	c.Assert(err, check.IsNil)

	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 202)

	var body map[string]any
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Assert(err, check.IsNil)
	id := body["change"].(string)

	st := d.Overlord().State()
	st.Lock()
	chg := st.Change(id)
	st.Unlock()
	c.Assert(chg, check.NotNil)

	<-chg.Ready()

	st.Lock()
	err = chg.Err()
	st.Unlock()
	c.Assert(err, check.IsNil)

	// Check that the configure hook was run correctly
	c.Check(hookRunner.Calls(), check.DeepEquals, [][]string{{
		"snap", "run", "--hook", "configure", "-r", "unset", "config-snap",
	}})
}

func (s *snapConfSuite) TestSetConfCoreSystemAlias(c *check.C) {
	d := s.daemon(c)
	s.mockSnap(c, `
name: core
version: 1
`)

	err := os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/etc/"), 0755)
	c.Assert(err, check.IsNil)
	err = os.WriteFile(filepath.Join(dirs.GlobalRootDir, "/etc/environment"), nil, 0644)
	c.Assert(err, check.IsNil)

	d.Overlord().Loop()
	defer d.Overlord().Stop()

	text, err := json.Marshal(map[string]any{"proxy.ftp": "value"})
	c.Assert(err, check.IsNil)

	buffer := bytes.NewBuffer(text)
	req, err := http.NewRequest("PUT", "/v2/snaps/system/conf", buffer)
	c.Assert(err, check.IsNil)

	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 202)

	var body map[string]any
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Assert(err, check.IsNil)
	id := body["change"].(string)

	st := d.Overlord().State()
	st.Lock()
	chg := st.Change(id)
	st.Unlock()
	c.Assert(chg, check.NotNil)

	<-chg.Ready()

	st.Lock()
	err = chg.Err()
	tr := config.NewTransaction(st)
	st.Unlock()
	c.Assert(err, check.IsNil)

	var value string
	tr.Get("core", "proxy.ftp", &value)
	c.Assert(value, check.Equals, "value")
}

func (s *snapConfSuite) TestSetConfNumber(c *check.C) {
	d := s.daemon(c)
	s.mockSnap(c, configYaml)

	// Mock the hook runner
	hookRunner := testutil.MockCommand(c, "snap", "")
	defer hookRunner.Restore()

	d.Overlord().Loop()
	defer d.Overlord().Stop()

	text, err := json.Marshal(map[string]any{"key": 1234567890})
	c.Assert(err, check.IsNil)

	buffer := bytes.NewBuffer(text)
	req, err := http.NewRequest("PUT", "/v2/snaps/config-snap/conf", buffer)
	c.Assert(err, check.IsNil)

	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 202)

	var body map[string]any
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Assert(err, check.IsNil)
	id := body["change"].(string)

	st := d.Overlord().State()
	st.Lock()
	chg := st.Change(id)
	st.Unlock()
	c.Assert(chg, check.NotNil)

	<-chg.Ready()

	st.Lock()
	defer st.Unlock()
	tr := config.NewTransaction(d.Overlord().State())
	var result any
	c.Assert(tr.Get("config-snap", "key", &result), check.IsNil)
	c.Assert(result, check.DeepEquals, json.Number("1234567890"))
}

func (s *snapConfSuite) TestSetConfBadSnap(c *check.C) {
	s.daemonWithOverlordMockAndStore()

	text, err := json.Marshal(map[string]any{"key": "value"})
	c.Assert(err, check.IsNil)

	buffer := bytes.NewBuffer(text)
	req, err := http.NewRequest("PUT", "/v2/snaps/config-snap/conf", buffer)
	c.Assert(err, check.IsNil)

	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 404)

	var body map[string]any
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Assert(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]any{
		"status-code": 404.,
		"status":      "Not Found",
		"result": map[string]any{
			"message": `snap "config-snap" is not installed`,
			"kind":    "snap-not-found",
			"value":   "config-snap",
		},
		"type": "error"})
}

func (s *snapConfSuite) TestSetConfChangeConflict(c *check.C) {
	s.daemon(c)
	s.mockSnap(c, configYaml)

	s.simulateConflict("config-snap")

	text, err := json.Marshal(map[string]any{"key": "value"})
	c.Assert(err, check.IsNil)

	buffer := bytes.NewBuffer(text)
	req, err := http.NewRequest("PUT", "/v2/snaps/config-snap/conf", buffer)
	c.Assert(err, check.IsNil)

	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 409)

	var body map[string]any
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Assert(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]any{
		"status-code": 409.,
		"status":      "Conflict",
		"result": map[string]any{
			"message": `snap "config-snap" has "manip" change in progress`,
			"kind":    "snap-change-conflict",
			"value": map[string]any{
				"change-kind": "manip",
				"snap-name":   "config-snap",
			},
		},
		"type": "error"})
}
