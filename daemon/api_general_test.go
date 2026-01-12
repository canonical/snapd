// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2024 Canonical Ltd
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
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/dirs/dirstest"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/sandbox"
	"github.com/snapcore/snapd/systemd"
)

var _ = check.Suite(&generalSuite{})

type generalSuite struct {
	apiBaseSuite
}

func (s *generalSuite) expectSystemInfoReadAccess() {
	s.expectReadAccess(daemon.InterfaceOpenAccess{Interfaces: []string{"snap-interfaces-requests-control"}})
}

func (s *generalSuite) expectSystemInfoWriteAccess() {
	s.expectWriteAccess(daemon.OpenAccess{})
}

func (s *generalSuite) expectChangesReadAccess() {
	s.expectReadAccess(daemon.InterfaceOpenAccess{Interfaces: []string{"snap-refresh-observe"}})
}

func (s *generalSuite) expectChangeReadAccess() {
	s.expectReadAccess(daemon.InterfaceOpenAccess{Interfaces: []string{"snap-refresh-observe", "ros-snapd-support"}})
}

func (s *generalSuite) expectSystemInfoStorageEncReadAccess() {
	s.expectReadAccess(daemon.OpenAccess{})
}

func (s *generalSuite) TestRoot(c *check.C) {
	s.daemon(c)

	req, err := http.NewRequest("GET", "/", nil)
	c.Assert(err, check.IsNil)

	// check it only does GET
	s.checkGetOnly(c, req)

	rec := httptest.NewRecorder()
	s.req(c, req, nil, actionIsExpected).ServeHTTP(rec, nil)
	c.Check(rec.Code, check.Equals, 200)
	c.Check(rec.Header().Get("Content-Type"), check.Equals, "application/json")

	expected := []any{"TBD"}
	var rsp daemon.RespJSON
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), check.IsNil)
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Result, check.DeepEquals, expected)
}

func (s *generalSuite) TestSysInfo(c *check.C) {
	s.expectSystemInfoReadAccess()
	req, err := http.NewRequest("GET", "/v2/system-info", nil)
	c.Assert(err, check.IsNil)

	restore := release.MockReleaseInfo(&release.OS{ID: "distro-id", VersionID: "1.2"})
	defer restore()
	restore = release.MockOnClassic(true)
	defer restore()
	restore = sandbox.MockForceDevMode(true)
	defer restore()

	r := c.MkDir()
	// using unknown distro, set up
	dirstest.MustMockAltSnapMountDir(r)
	dirstest.MustMockClassicConfinementAltDirSupport(r)
	// reload dirs for release info to have effect
	dirs.SetRootDir(r)

	restore = daemon.MockSystemdVirt("magic")
	defer restore()
	// Set systemd version <230 so QuotaGroups feature unsupported
	restore = systemd.MockSystemdVersion(229, nil)
	defer restore()

	buildID := "this-is-my-build-id"
	restore = daemon.MockBuildID(buildID)
	defer restore()

	d := s.daemon(c)
	d.Version = "42b1"

	// set both legacy and new refresh schedules. new one takes priority
	st := d.Overlord().State()
	st.Lock()
	tr := config.NewTransaction(st)
	tr.Set("core", "refresh.schedule", "00:00-9:00/12:00-13:00")
	tr.Set("core", "refresh.timer", "8:00~9:00/2")
	tr.Set("core", "experimental.parallel-instances", "false")
	tr.Set("core", "experimental.quota-groups", "true")
	tr.Commit()
	st.Unlock()

	s.expectSystemInfoReadAccess()

	rec := httptest.NewRecorder()
	s.req(c, req, nil, actionIsExpected).ServeHTTP(rec, nil)
	c.Check(rec.Code, check.Equals, 200)
	c.Check(rec.Header().Get("Content-Type"), check.Equals, "application/json")

	expected := map[string]any{
		"series":  "16",
		"version": "42b1",
		"os-release": map[string]any{
			"id":         "distro-id",
			"version-id": "1.2",
		},
		"build-id":   buildID,
		"on-classic": true,
		"managed":    false,
		"locations": map[string]any{
			"snap-mount-dir": dirs.SnapMountDir,
			"snap-bin-dir":   dirs.SnapBinariesDir,
		},
		"refresh": map[string]any{
			// only the "timer" field
			"timer": "8:00~9:00/2",
		},
		"confinement":      "partial",
		"sandbox-features": map[string]any{"confinement-options": []any{"classic", "devmode"}},
		"architecture":     arch.DpkgArchitecture(),
		"virtualization":   "magic",
		"system-mode":      "run",
		"snapd-bin-from":   "native-package",
	}
	var rsp daemon.RespJSON
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), check.IsNil)
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Type, check.Equals, daemon.ResponseTypeSync)
	// Ensure that we had a kernel-verrsion but don't check the actual value.
	const kernelVersionKey = "kernel-version"
	c.Check(rsp.Result.(map[string]any)[kernelVersionKey], check.Not(check.Equals), "")
	delete(rsp.Result.(map[string]any), kernelVersionKey)
	// Extract "features" field and remove it from result; check it later.
	const featuresKey = "features"
	resultFeatures := rsp.Result.(map[string]any)[featuresKey]
	c.Check(resultFeatures, check.Not(check.Equals), "")
	delete(rsp.Result.(map[string]any), featuresKey)

	c.Check(rsp.Result, check.DeepEquals, expected)

	// Check that "features" is map
	featuresAll, ok := resultFeatures.(map[string]any)
	c.Assert(ok, check.Equals, true)
	// Ensure that Layouts exists and is feature.FeatureInfo
	layoutsInfoRaw, exists := featuresAll[features.Layouts.String()]
	c.Assert(exists, check.Equals, true)
	layoutsInfo, ok := layoutsInfoRaw.(map[string]any)
	c.Assert(ok, check.Equals, true, check.Commentf("%+v", layoutsInfoRaw))
	// Ensure that Layouts is supported and enabled
	c.Check(layoutsInfo["supported"], check.Equals, true)
	_, exists = layoutsInfo["unsupported-reason"]
	c.Check(exists, check.Equals, false)
	c.Check(layoutsInfo["enabled"], check.Equals, true)
	// Ensure that ParallelInstances exists and is a feature.FeatureInfo
	parallelInstancesInfoRaw, exists := featuresAll[features.ParallelInstances.String()]
	c.Assert(exists, check.Equals, true)
	parallelInstancesInfo, ok := parallelInstancesInfoRaw.(map[string]any)
	c.Assert(ok, check.Equals, true)
	// Ensure that ParallelInstances is supported and not enabled
	c.Check(parallelInstancesInfo["supported"], check.Equals, true)
	_, exists = parallelInstancesInfo["unsupported-reason"]
	c.Check(exists, check.Equals, false)
	c.Check(parallelInstancesInfo["enabled"], check.Equals, false)
	// Ensure that QuotaGroups exists and is a feature.FeatureInfo
	quotaGroupsInfoRaw, exists := featuresAll[features.QuotaGroups.String()]
	c.Assert(exists, check.Equals, true)
	quotaGroupsInfo, ok := quotaGroupsInfoRaw.(map[string]any)
	c.Assert(ok, check.Equals, true)
	// Ensure that QuotaGroups is unsupported but enabled
	c.Check(quotaGroupsInfo["supported"], check.Equals, false)
	unsupportedReason, exists := quotaGroupsInfo["unsupported-reason"]
	c.Check(exists, check.Equals, true)
	c.Check(unsupportedReason, check.Not(check.Equals), "")
	c.Check(quotaGroupsInfo["enabled"], check.Equals, true)
}

func (s *generalSuite) TestSysInfoLegacyRefresh(c *check.C) {
	s.expectSystemInfoReadAccess()
	req, err := http.NewRequest("GET", "/v2/system-info", nil)
	c.Assert(err, check.IsNil)

	restore := release.MockReleaseInfo(&release.OS{ID: "distro-id", VersionID: "1.2"})
	defer restore()
	restore = release.MockOnClassic(true)
	defer restore()
	restore = sandbox.MockForceDevMode(true)
	defer restore()
	restore = daemon.MockSystemdVirt("kvm")
	defer restore()

	buildID := "this-is-my-build-id"
	restore = daemon.MockBuildID(buildID)
	defer restore()

	r := c.MkDir()
	// using unknown distro, set up
	dirstest.MustMockAltSnapMountDir(r)
	dirstest.MustMockClassicConfinementAltDirSupport(r)
	// reload dirs for release info to have effect
	dirs.SetRootDir(r)

	d := s.daemon(c)
	d.Version = "42b1"

	// set the legacy refresh schedule
	st := d.Overlord().State()
	st.Lock()
	tr := config.NewTransaction(st)
	tr.Set("core", "refresh.schedule", "00:00-9:00/12:00-13:00")
	tr.Set("core", "refresh.timer", "")
	tr.Commit()
	st.Unlock()

	// add a test security backend
	err = d.Overlord().InterfaceManager().Repository().AddBackend(&ifacetest.TestSecurityBackend{
		BackendName:             "apparmor",
		SandboxFeaturesCallback: func() []string { return []string{"feature-1", "feature-2"} },
	})
	c.Assert(err, check.IsNil)

	s.expectSystemInfoReadAccess()

	rec := httptest.NewRecorder()
	s.req(c, req, nil, actionIsExpected).ServeHTTP(rec, nil)
	c.Check(rec.Code, check.Equals, 200)
	c.Check(rec.Header().Get("Content-Type"), check.Equals, "application/json")

	expected := map[string]any{
		"series":  "16",
		"version": "42b1",
		"os-release": map[string]any{
			"id":         "distro-id",
			"version-id": "1.2",
		},
		"build-id":   buildID,
		"on-classic": true,
		"managed":    false,
		"locations": map[string]any{
			"snap-mount-dir": dirs.SnapMountDir,
			"snap-bin-dir":   dirs.SnapBinariesDir,
		},
		"refresh": map[string]any{
			// only the "schedule" field
			"schedule": "00:00-9:00/12:00-13:00",
		},
		"confinement": "partial",
		"sandbox-features": map[string]any{
			"apparmor":            []any{"feature-1", "feature-2"},
			"confinement-options": []any{"classic", "devmode"}, // we know it's this because of the release.Mock... calls above
		},
		"architecture":   arch.DpkgArchitecture(),
		"virtualization": "kvm",
		"system-mode":    "run",
		"snapd-bin-from": "native-package",
	}
	var rsp daemon.RespJSON
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), check.IsNil)
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Type, check.Equals, daemon.ResponseTypeSync)
	const kernelVersionKey = "kernel-version"
	delete(rsp.Result.(map[string]any), kernelVersionKey)
	const featuresKey = "features"
	delete(rsp.Result.(map[string]any), featuresKey)
	c.Check(rsp.Result, check.DeepEquals, expected)
}

func (s *generalSuite) testSysInfoBinOrigin(c *check.C, exp string, expErr string) {
	s.expectSystemInfoReadAccess()
	req, err := http.NewRequest("GET", "/v2/system-info", nil)
	c.Assert(err, check.IsNil)

	restore := release.MockReleaseInfo(&release.OS{ID: "distro-id", VersionID: "1.2"})
	defer restore()
	restore = release.MockOnClassic(true)
	defer restore()
	restore = sandbox.MockForceDevMode(true)
	defer restore()

	r := c.MkDir()
	// using unknown distro, set up
	dirstest.MustMockAltSnapMountDir(r)
	dirstest.MustMockClassicConfinementAltDirSupport(r)
	// reload dirs for release info to have effect
	dirs.SetRootDir(r)

	restore = daemon.MockSystemdVirt("magic")
	defer restore()
	// Set systemd version <230 so QuotaGroups feature unsupported
	restore = systemd.MockSystemdVersion(229, nil)
	defer restore()

	buildID := "this-is-my-build-id"
	restore = daemon.MockBuildID(buildID)
	defer restore()

	d := s.daemon(c)
	d.Version = "42b1"

	s.expectSystemInfoReadAccess()

	rec := httptest.NewRecorder()
	s.req(c, req, nil, actionIsExpected).ServeHTTP(rec, nil)
	if expErr == "" {
		c.Check(rec.Code, check.Equals, 200)
		c.Check(rec.Header().Get("Content-Type"), check.Equals, "application/json")

		var rsp daemon.RespJSON
		c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), check.IsNil)
		c.Check(rsp.Status, check.Equals, 200)
		c.Check(rsp.Type, check.Equals, daemon.ResponseTypeSync)

		m, _ := rsp.Result.(map[string]any)
		c.Assert(m, check.NotNil)
		c.Check(m["snapd-bin-from"], check.Equals, exp)
	} else {
		c.Check(rec.Code, check.Equals, 500)
		c.Check(rec.Header().Get("Content-Type"), check.Equals, "application/json")
		assertResponseBody(c, rec.Body, map[string]any{
			"type":        "error",
			"status-code": float64(500),
			"status":      "Internal Server Error",
			"result": map[string]any{
				"message": `cannot obtain snapd reexec status: mock error`,
			},
		})
	}
}

func (s *generalSuite) TestSysInfoBinOriginSnapd(c *check.C) {
	defer daemon.MockSnapdtoolsIsReexecd(func() (bool, error) {
		return true, nil
	})()

	s.testSysInfoBinOrigin(c, "snap", "")
}

func (s *generalSuite) TestSysInfoBinOriginError(c *check.C) {
	defer daemon.MockSnapdtoolsIsReexecd(func() (bool, error) {
		return false, errors.New("mock error")
	})()

	s.testSysInfoBinOrigin(c, "", "foo bar")
}

func (s *generalSuite) testSysInfoSystemMode(c *check.C, mode string) {
	s.expectSystemInfoReadAccess()
	req, err := http.NewRequest("GET", "/v2/system-info", nil)
	c.Assert(err, check.IsNil)

	c.Assert(mode != "", check.Equals, true, check.Commentf("mode is unset for the test"))

	restore := release.MockReleaseInfo(&release.OS{ID: "distro-id", VersionID: "1.2"})
	defer restore()
	restore = release.MockOnClassic(false)
	defer restore()
	restore = sandbox.MockForceDevMode(false)
	defer restore()
	restore = daemon.MockSystemdVirt("")
	defer restore()

	// reload dirs for release info to have effect on paths
	dirs.SetRootDir(dirs.GlobalRootDir)

	// mock the modeenv file
	m := boot.Modeenv{
		Mode:           mode,
		RecoverySystem: "20191127",
	}
	c.Assert(m.WriteTo(""), check.IsNil)

	d := s.daemon(c)
	d.Version = "42b1"

	// add a test security backend
	err = d.Overlord().InterfaceManager().Repository().AddBackend(&ifacetest.TestSecurityBackend{
		BackendName:             "apparmor",
		SandboxFeaturesCallback: func() []string { return []string{"feature-1", "feature-2"} },
	})
	c.Assert(err, check.IsNil)

	buildID := "this-is-my-build-id"
	restore = daemon.MockBuildID(buildID)
	defer restore()

	rec := httptest.NewRecorder()
	s.req(c, req, nil, actionIsExpected).ServeHTTP(rec, nil)
	c.Check(rec.Code, check.Equals, 200)
	c.Check(rec.Header().Get("Content-Type"), check.Equals, "application/json")

	expected := map[string]any{
		"series":  "16",
		"version": "42b1",
		"os-release": map[string]any{
			"id":         "distro-id",
			"version-id": "1.2",
		},
		"build-id":   buildID,
		"on-classic": false,
		"managed":    false,
		"locations": map[string]any{
			"snap-mount-dir": dirs.SnapMountDir,
			"snap-bin-dir":   dirs.SnapBinariesDir,
		},
		"refresh": map[string]any{
			"timer": "00:00~24:00/4",
		},
		"confinement": "strict",
		"sandbox-features": map[string]any{
			"apparmor":            []any{"feature-1", "feature-2"},
			"confinement-options": []any{"devmode", "strict"}, // we know it's this because of the release.Mock... calls above
		},
		"architecture":   arch.DpkgArchitecture(),
		"system-mode":    mode,
		"snapd-bin-from": "native-package",
	}
	var rsp daemon.RespJSON
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), check.IsNil)
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Type, check.Equals, daemon.ResponseTypeSync)
	const kernelVersionKey = "kernel-version"
	delete(rsp.Result.(map[string]any), kernelVersionKey)
	const featuresKey = "features"
	delete(rsp.Result.(map[string]any), featuresKey)
	c.Check(rsp.Result, check.DeepEquals, expected)
}

func (s *generalSuite) TestSysInfoSystemModeRun(c *check.C) {
	s.testSysInfoSystemMode(c, "run")
}

func (s *generalSuite) TestSysInfoSystemModeRecover(c *check.C) {
	s.testSysInfoSystemMode(c, "recover")
}

func (s *generalSuite) TestSysInfoSystemModeInstall(c *check.C) {
	s.testSysInfoSystemMode(c, "install")
}
func (s *generalSuite) TestSysInfoIsManaged(c *check.C) {
	s.expectSystemInfoReadAccess()
	d := s.daemon(c)

	st := d.Overlord().State()
	st.Lock()
	_, err := auth.NewUser(st, auth.NewUserParams{
		Username:   "someuser",
		Email:      "mymail@test.com",
		Macaroon:   "macaroon",
		Discharges: []string{"discharge"},
	})
	st.Unlock()
	c.Assert(err, check.IsNil)

	req, err := http.NewRequest("GET", "/v2/system-info", nil)
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil, actionIsExpected)
	c.Check(rsp.Result.(map[string]any)["managed"], check.Equals, true)
}

func (s *generalSuite) TestSysInfoWorksDegraded(c *check.C) {
	s.expectSystemInfoReadAccess()
	d := s.daemon(c)

	d.SetDegradedMode(fmt.Errorf("some error"))

	req, err := http.NewRequest("GET", "/v2/system-info", nil)
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil, actionIsExpected)
	c.Check(rsp.Status, check.Equals, 200)
}

func (s *generalSuite) TestSysInfoClientAdviceProceedMatchingKey(c *check.C) {
	s.expectSystemInfoWriteAccess()
	s.daemon(c)

	s.AddCleanup(interfaces.MockSystemKey(`
{
"build-id": "7a94e9736c091b3984bd63f5aebfc883c4d859e0",
"apparmor-features": ["caps", "dbus"]
}`))
	c.Assert(interfaces.WriteSystemKey(interfaces.SystemKeyExtraData{}), check.IsNil)

	k, err := interfaces.RecordedSystemKey()
	c.Assert(err, check.IsNil)
	ks := k.(fmt.Stringer).String()

	b, err := json.Marshal(map[string]string{
		"action":     "advise-system-key-mismatch",
		"system-key": ks,
	})
	c.Assert(err, check.IsNil)
	req, err := http.NewRequest("POST", "/v2/system-info", bytes.NewReader(b))
	c.Assert(err, check.IsNil)

	// no need to start overlord's loop as the key is a match
	rec := httptest.NewRecorder()
	s.req(c, req, nil, actionIsExpected).ServeHTTP(rec, nil)
	c.Logf("rec: %v", rec)
	c.Check(rec.Code, check.Equals, 200)
}

func (s *generalSuite) TestSysInfoClientAdviceAwaitChangeMismatch(c *check.C) {
	s.expectSystemInfoWriteAccess()
	d := s.daemon(c)
	s.AddCleanup(interfaces.MockSystemKey(`
{
"build-id": "7a94e9736c091b3984bd63f5aebfc883c4d859e0",
"apparmor-features": ["caps", "dbus"]
}`))
	c.Assert(interfaces.WriteSystemKey(interfaces.SystemKeyExtraData{}), check.IsNil)

	k, err := interfaces.RecordedSystemKey()
	c.Assert(err, check.IsNil)
	ks := k.(fmt.Stringer).String()

	// write a changed system key to cause a mismatch so that snapd and client
	// observe different values
	s.AddCleanup(interfaces.MockSystemKey(`
{
"build-id": "7a94e9736c091b3984bd63f5aebfc883c4d859e0",
"apparmor-features": ["caps", "dbus", "new-feature"]
}`))
	c.Assert(interfaces.WriteSystemKey(interfaces.SystemKeyExtraData{}), check.IsNil)

	b, err := json.Marshal(map[string]string{
		"action":     "advise-system-key-mismatch",
		"system-key": ks,
	})
	c.Assert(err, check.IsNil)
	req, err := http.NewRequest("POST", "/v2/system-info", bytes.NewReader(b))
	c.Assert(err, check.IsNil)

	d.Overlord().Loop()
	defer d.Overlord().Stop()

	rec := httptest.NewRecorder()
	s.req(c, req, nil, actionIsExpected).ServeHTTP(rec, nil)
	c.Check(rec.Code, check.Equals, 202)

	var body map[string]any
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	id := body["change"].(string)
	st := d.Overlord().State()
	st.Lock()
	chg := st.Change(id)
	c.Check(chg.Summary(), check.Equals, `Regenerate security profiles`)
	st.Unlock()
	c.Assert(chg, check.NotNil)
}

func (s *generalSuite) TestSysInfoClientAdviceInternalError(c *check.C) {
	s.expectSystemInfoWriteAccess()
	s.daemon(c)
	s.AddCleanup(interfaces.MockSystemKey(`
{
"build-id": "7a94e9736c091b3984bd63f5aebfc883c4d859e0",
"apparmor-features": ["caps", "dbus"]
}`))
	c.Assert(interfaces.WriteSystemKey(interfaces.SystemKeyExtraData{}), check.IsNil)
	k, err := interfaces.RecordedSystemKey()
	c.Assert(err, check.IsNil)
	ks := k.(fmt.Stringer).String()

	// no system key, will cause internal error from ifacestate
	c.Assert(interfaces.RemoveSystemKey(), check.IsNil)

	b, err := json.Marshal(map[string]string{
		"action":     "advise-system-key-mismatch",
		"system-key": ks,
	})
	c.Assert(err, check.IsNil)
	req, err := http.NewRequest("POST", "/v2/system-info", bytes.NewReader(b))
	c.Assert(err, check.IsNil)

	// no need to start overlord's loop as no change is added
	rec := httptest.NewRecorder()
	s.req(c, req, nil, actionIsExpected).ServeHTTP(rec, nil)
	c.Check(rec.Code, check.Equals, 500)
	assertResponseBody(c, rec.Body, map[string]any{
		"type":        "error",
		"status-code": float64(500),
		"status":      "Internal Server Error",
		"result": map[string]any{
			"message": `cannot process system key: system-key missing on disk`,
		},
	})
}

func (s *generalSuite) TestSysInfoClientAdviceUnsupportedSystemKey(c *check.C) {
	s.expectSystemInfoWriteAccess()
	s.daemon(c)
	s.AddCleanup(interfaces.MockSystemKey(`
{
"version": 999,
"build-id": "7a94e9736c091b3984bd63f5aebfc883c4d859e0",
"apparmor-features": ["caps", "dbus"]
}`))
	c.Assert(interfaces.WriteSystemKey(interfaces.SystemKeyExtraData{}), check.IsNil)
	k, err := interfaces.RecordedSystemKey()
	c.Assert(err, check.IsNil)
	ks := k.(fmt.Stringer).String()

	// actual snapd system key is of lower version
	s.AddCleanup(interfaces.MockSystemKey(`
{
"version": 11,
"build-id": "7a94e9736c091b3984bd63f5aebfc883c4d859e0",
"apparmor-features": ["caps", "dbus"]
}`))
	c.Assert(interfaces.WriteSystemKey(interfaces.SystemKeyExtraData{}), check.IsNil)

	b, err := json.Marshal(map[string]string{
		"action":     "advise-system-key-mismatch",
		"system-key": ks,
	})
	c.Assert(err, check.IsNil)
	req, err := http.NewRequest("POST", "/v2/system-info", bytes.NewReader(b))
	c.Assert(err, check.IsNil)

	// no need to start overlord's loop as no change is added
	rec := httptest.NewRecorder()
	s.req(c, req, nil, actionIsExpected).ServeHTTP(rec, nil)
	c.Check(rec.Code, check.Equals, 400)
	assertResponseBody(c, rec.Body, map[string]any{
		"type":        "error",
		"status-code": float64(400),
		"status":      "Bad Request",
		"result": map[string]any{
			"kind":    "unsupported-system-key-version",
			"message": "system-key version higher than supported",
			"value":   "",
		},
	})
}

func (s *generalSuite) TestSysInfoClientAdviceBadRequest(c *check.C) {
	s.expectSystemInfoWriteAccess()
	s.daemon(c)
	s.AddCleanup(interfaces.MockSystemKey(`
{
"build-id": "7a94e9736c091b3984bd63f5aebfc883c4d859e0",
"apparmor-features": ["caps", "dbus"]
}`))
	c.Assert(interfaces.WriteSystemKey(interfaces.SystemKeyExtraData{}), check.IsNil)

	b, err := json.Marshal(map[string]string{
		"action":     "advise-system-key-mismatch",
		"system-key": "not-a-system-key",
	})
	c.Assert(err, check.IsNil)
	req, err := http.NewRequest("POST", "/v2/system-info", bytes.NewReader(b))
	c.Assert(err, check.IsNil)

	// no need to start overlord's loop as the key is a match
	rec := httptest.NewRecorder()
	s.req(c, req, nil, actionIsExpected).ServeHTTP(rec, nil)
	c.Check(rec.Code, check.Equals, 400)
	assertResponseBody(c, rec.Body, map[string]any{
		"type":        "error",
		"status-code": float64(400),
		"status":      "Bad Request",
		"result": map[string]any{
			"message": `cannot decode system key: invalid character 'o' in literal null (expecting 'u')`,
		},
	})

	req, err = http.NewRequest("POST", "/v2/system-info", bytes.NewReader([]byte(`{}{}`)))
	c.Assert(err, check.IsNil)

	// no need to start overlord's loop as the key is a match
	rec = httptest.NewRecorder()
	s.req(c, req, nil, actionIsExpected).ServeHTTP(rec, nil)
	c.Check(rec.Code, check.Equals, 400)
	assertResponseBody(c, rec.Body, map[string]any{
		"type":        "error",
		"status-code": float64(400),
		"status":      "Bad Request",
		"result": map[string]any{
			"message": `unexpected additional content in request body`,
		},
	})

	// no system key at all
	b, err = json.Marshal(map[string]string{
		"action": "advise-system-key-mismatch",
	})
	c.Assert(err, check.IsNil)
	req, err = http.NewRequest("POST", "/v2/system-info", bytes.NewReader(b))
	c.Assert(err, check.IsNil)

	// no need to start overlord's loop as the key is a match
	rec = httptest.NewRecorder()
	s.req(c, req, nil, actionIsExpected).ServeHTTP(rec, nil)
	c.Check(rec.Code, check.Equals, 400)
	assertResponseBody(c, rec.Body, map[string]any{
		"type":        "error",
		"status-code": float64(400),
		"status":      "Bad Request",
		"result": map[string]any{
			"message": `cannot decode system key: EOF`,
		},
	})
}

func (s *generalSuite) TestSysInfoPostAction(c *check.C) {
	s.expectSystemInfoWriteAccess()
	s.daemon(c)

	// unknown action
	b, err := json.Marshal(map[string]string{
		"action": "unexpected",
	})
	c.Assert(err, check.IsNil)
	req, err := http.NewRequest("POST", "/v2/system-info", bytes.NewReader(b))
	c.Assert(err, check.IsNil)

	// no need to start overlord's loop as the key is a match
	rec := httptest.NewRecorder()
	s.req(c, req, nil, actionIsUnexpected).ServeHTTP(rec, nil)
	c.Check(rec.Code, check.Equals, 400)
	assertResponseBody(c, rec.Body, map[string]any{
		"type":        "error",
		"status-code": float64(400),
		"status":      "Bad Request",
		"result": map[string]any{
			"message": `unsupported action "unexpected"`,
		},
	})
	// no action at all
	b, err = json.Marshal(map[string]string{})
	c.Assert(err, check.IsNil)
	req, err = http.NewRequest("POST", "/v2/system-info", bytes.NewReader(b))
	c.Assert(err, check.IsNil)

	// no need to start overlord's loop as the key is a match
	rec = httptest.NewRecorder()
	s.req(c, req, nil, actionIsExpected).ServeHTTP(rec, nil)
	c.Check(rec.Code, check.Equals, 400)
	assertResponseBody(c, rec.Body, map[string]any{
		"type":        "error",
		"status-code": float64(400),
		"status":      "Bad Request",
		"result": map[string]any{
			"message": `no action`,
		},
	})
}

func setupChanges(st *state.State) []string {
	chg1 := st.NewChange("install", "install...")
	chg1.Set("snap-names", []string{"funky-snap-name"})
	t1 := st.NewTask("download", "1...")
	t2 := st.NewTask("activate", "2...")
	chg1.AddAll(state.NewTaskSet(t1, t2))
	t1.Logf("l11")
	t1.Logf("l12")
	chg2 := st.NewChange("remove", "remove..")
	t3 := st.NewTask("unlink", "1...")
	chg2.AddTask(t3)
	t3.SetStatus(state.ErrorStatus)
	t3.Errorf("rm failed")

	return []string{chg1.ID(), chg2.ID(), t1.ID(), t2.ID(), t3.ID()}
}

func (s *generalSuite) TestStateChangesDefaultToInProgress(c *check.C) {
	restore := state.MockTime(time.Date(2016, 04, 21, 1, 2, 3, 0, time.UTC))
	defer restore()

	// Setup
	s.expectChangesReadAccess()
	d := s.daemon(c)
	st := d.Overlord().State()
	st.Lock()
	setupChanges(st)
	st.Unlock()

	// Execute
	req, err := http.NewRequest("GET", "/v2/changes", nil)
	c.Assert(err, check.IsNil)
	rsp := s.syncReq(c, req, nil, actionIsExpected)

	// Verify
	c.Check(rsp.Status, check.Equals, 200)
	c.Assert(rsp.Result, check.HasLen, 1)

	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, nil)
	c.Assert(rec.Code, check.Equals, 200)
	res := rec.Body.Bytes()

	c.Check(string(res), check.Matches, `.*{"id":"\w+","kind":"install","summary":"install...","status":"Do","tasks":\[{"id":"\w+","kind":"download","summary":"1...","status":"Do","log":\["2016-04-21T01:02:03Z INFO l11","2016-04-21T01:02:03Z INFO l12"],"progress":{"label":"","done":0,"total":1},"spawn-time":"2016-04-21T01:02:03Z"}.*`)
}

func (s *generalSuite) TestStateChangesInProgress(c *check.C) {
	restore := state.MockTime(time.Date(2016, 04, 21, 1, 2, 3, 0, time.UTC))
	defer restore()

	// Setup
	s.expectChangesReadAccess()
	d := s.daemon(c)
	st := d.Overlord().State()
	st.Lock()
	setupChanges(st)
	st.Unlock()

	// Execute
	req, err := http.NewRequest("GET", "/v2/changes?select=in-progress", nil)
	c.Assert(err, check.IsNil)
	rsp := s.syncReq(c, req, nil, actionIsExpected)

	// Verify
	c.Check(rsp.Status, check.Equals, 200)
	c.Assert(rsp.Result, check.HasLen, 1)

	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, nil)
	c.Assert(rec.Code, check.Equals, 200)
	res := rec.Body.Bytes()

	c.Check(string(res), check.Matches, `.*{"id":"\w+","kind":"install","summary":"install...","status":"Do","tasks":\[{"id":"\w+","kind":"download","summary":"1...","status":"Do","log":\["2016-04-21T01:02:03Z INFO l11","2016-04-21T01:02:03Z INFO l12"],"progress":{"label":"","done":0,"total":1},"spawn-time":"2016-04-21T01:02:03Z"}.*],"ready":false,"spawn-time":"2016-04-21T01:02:03Z"}.*`)
}

func (s *generalSuite) TestStateChangesAll(c *check.C) {
	restore := state.MockTime(time.Date(2016, 04, 21, 1, 2, 3, 0, time.UTC))
	defer restore()

	// Setup
	s.expectChangesReadAccess()
	d := s.daemon(c)
	st := d.Overlord().State()
	st.Lock()
	setupChanges(st)
	st.Unlock()

	// Execute
	req, err := http.NewRequest("GET", "/v2/changes?select=all", nil)
	c.Assert(err, check.IsNil)
	rsp := s.syncReq(c, req, nil, actionIsExpected)

	// Verify
	c.Check(rsp.Status, check.Equals, 200)
	c.Assert(rsp.Result, check.HasLen, 2)

	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, nil)
	c.Assert(rec.Code, check.Equals, 200)
	res := rec.Body.Bytes()

	c.Check(string(res), check.Matches, `.*{"id":"\w+","kind":"install","summary":"install...","status":"Do","tasks":\[{"id":"\w+","kind":"download","summary":"1...","status":"Do","log":\["2016-04-21T01:02:03Z INFO l11","2016-04-21T01:02:03Z INFO l12"],"progress":{"label":"","done":0,"total":1},"spawn-time":"2016-04-21T01:02:03Z"}.*],"ready":false,"spawn-time":"2016-04-21T01:02:03Z"}.*`)
	c.Check(string(res), check.Matches, `.*{"id":"\w+","kind":"remove","summary":"remove..","status":"Error","tasks":\[{"id":"\w+","kind":"unlink","summary":"1...","status":"Error","log":\["2016-04-21T01:02:03Z ERROR rm failed"],"progress":{"label":"","done":1,"total":1},"spawn-time":"2016-04-21T01:02:03Z","ready-time":"2016-04-21T01:02:03Z"}.*],"ready":true,"err":"[^"]+".*`)
}

func (s *generalSuite) TestStateChangesReady(c *check.C) {
	restore := state.MockTime(time.Date(2016, 04, 21, 1, 2, 3, 0, time.UTC))
	defer restore()

	// Setup
	s.expectChangesReadAccess()
	d := s.daemon(c)
	st := d.Overlord().State()
	st.Lock()
	setupChanges(st)
	st.Unlock()

	// Execute
	req, err := http.NewRequest("GET", "/v2/changes?select=ready", nil)
	c.Assert(err, check.IsNil)
	rsp := s.syncReq(c, req, nil, actionIsExpected)

	// Verify
	c.Check(rsp.Status, check.Equals, 200)
	c.Assert(rsp.Result, check.HasLen, 1)

	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, nil)
	c.Assert(rec.Code, check.Equals, 200)
	res := rec.Body.Bytes()

	c.Check(string(res), check.Matches, `.*{"id":"\w+","kind":"remove","summary":"remove..","status":"Error","tasks":\[{"id":"\w+","kind":"unlink","summary":"1...","status":"Error","log":\["2016-04-21T01:02:03Z ERROR rm failed"],"progress":{"label":"","done":1,"total":1},"spawn-time":"2016-04-21T01:02:03Z","ready-time":"2016-04-21T01:02:03Z"}.*],"ready":true,"err":"[^"]+".*`)
}

func (s *generalSuite) TestStateChangesForSnapName(c *check.C) {
	restore := state.MockTime(time.Date(2016, 04, 21, 1, 2, 3, 0, time.UTC))
	defer restore()

	// Setup
	s.expectChangesReadAccess()
	d := s.daemon(c)
	st := d.Overlord().State()
	st.Lock()
	setupChanges(st)
	st.Unlock()

	// Execute
	req, err := http.NewRequest("GET", "/v2/changes?for=funky-snap-name&select=all", nil)
	c.Assert(err, check.IsNil)
	rsp := s.syncReq(c, req, nil, actionIsExpected)

	// Verify
	c.Check(rsp.Status, check.Equals, 200)
	c.Assert(rsp.Result, check.FitsTypeOf, []*daemon.ChangeInfo(nil))

	res := rsp.Result.([]*daemon.ChangeInfo)
	c.Assert(res, check.HasLen, 1)
	c.Check(res[0].Kind, check.Equals, `install`)

	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, nil)
	c.Assert(rec.Code, check.Equals, 200)
}

func (s *generalSuite) TestStateChangesForSnapNameWithApp(c *check.C) {
	restore := state.MockTime(time.Date(2016, 04, 21, 1, 2, 3, 0, time.UTC))
	defer restore()

	// Setup
	s.expectChangesReadAccess()
	d := s.daemon(c)
	st := d.Overlord().State()
	st.Lock()
	chg1 := st.NewChange("service-control", "install...")
	// as triggered by snap restart lxd.daemon
	chg1.Set("snap-names", []string{"lxd.daemon"})
	t1 := st.NewTask("exec-command", "1...")
	chg1.AddAll(state.NewTaskSet(t1))
	t1.Logf("foobar")

	st.Unlock()

	// Execute
	req, err := http.NewRequest("GET", "/v2/changes?for=lxd&select=all", nil)
	c.Assert(err, check.IsNil)
	rsp := s.syncReq(c, req, nil, actionIsExpected)

	// Verify
	c.Check(rsp.Status, check.Equals, 200)
	c.Assert(rsp.Result, check.FitsTypeOf, []*daemon.ChangeInfo(nil))

	res := rsp.Result.([]*daemon.ChangeInfo)
	c.Assert(res, check.HasLen, 1)
	c.Check(res[0].Kind, check.Equals, `service-control`)

	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, nil)
	c.Assert(rec.Code, check.Equals, 200)
}

func (s *generalSuite) TestStateChange(c *check.C) {
	restore := state.MockTime(time.Date(2016, 04, 21, 1, 2, 3, 0, time.UTC))
	defer restore()

	// Setup
	s.expectChangeReadAccess()
	d := s.daemon(c)
	st := d.Overlord().State()
	st.Lock()
	ids := setupChanges(st)
	chg := st.Change(ids[0])
	chg.Set("api-data", map[string]int{"n": 42})
	st.Unlock()

	restore = daemon.MockSnapstateSnapsAffectedByTask(func(t *state.Task) ([]string, error) {
		switch t.Kind() {
		case "download":
			// Mock affected snaps
			return []string{"some-snap"}, nil
		case "activate":
			// Error should be ignored
			return nil, fmt.Errorf("boom")
		default:
			return nil, nil
		}
	})
	defer restore()

	// Execute
	req, err := http.NewRequest("GET", "/v2/changes/"+ids[0], nil)
	c.Assert(err, check.IsNil)
	rsp := s.syncReq(c, req, nil, actionIsExpected)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)

	// Verify
	c.Check(rec.Code, check.Equals, 200)
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Result, check.NotNil)

	var body map[string]any
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body["result"], check.DeepEquals, map[string]any{
		"id":         ids[0],
		"kind":       "install",
		"summary":    "install...",
		"status":     "Do",
		"ready":      false,
		"spawn-time": "2016-04-21T01:02:03Z",
		"tasks": []any{
			map[string]any{
				"id":         ids[2],
				"kind":       "download",
				"summary":    "1...",
				"status":     "Do",
				"log":        []any{"2016-04-21T01:02:03Z INFO l11", "2016-04-21T01:02:03Z INFO l12"},
				"progress":   map[string]any{"label": "", "done": 0., "total": 1.},
				"spawn-time": "2016-04-21T01:02:03Z",
				"data":       map[string]any{"affected-snaps": []any{"some-snap"}},
			},
			map[string]any{
				"id":         ids[3],
				"kind":       "activate",
				"summary":    "2...",
				"status":     "Do",
				"progress":   map[string]any{"label": "", "done": 0., "total": 1.},
				"spawn-time": "2016-04-21T01:02:03Z",
			},
		},
		"data": map[string]any{
			"n": float64(42),
		},
	})
}

func (s *generalSuite) expectManageAccess() {
	s.expectWriteAccess(daemon.AuthenticatedAccess{Polkit: "io.snapcraft.snapd.manage"})
}

func (s *generalSuite) TestStateChangeAbort(c *check.C) {
	restore := state.MockTime(time.Date(2016, 04, 21, 1, 2, 3, 0, time.UTC))
	defer restore()

	soon := 0
	_, restore = daemon.MockEnsureStateSoon(func(st *state.State) {
		soon++
	})
	defer restore()

	// Setup
	s.expectChangeReadAccess()
	d := s.daemon(c)
	st := d.Overlord().State()
	st.Lock()
	ids := setupChanges(st)
	st.Unlock()

	s.expectManageAccess()

	buf := bytes.NewBufferString(`{"action": "abort"}`)

	// Execute
	req, err := http.NewRequest("POST", "/v2/changes/"+ids[0], buf)
	c.Assert(err, check.IsNil)
	rsp := s.syncReq(c, req, nil, actionIsExpected)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)

	// Ensure scheduled
	c.Check(soon, check.Equals, 1)

	// Verify
	c.Check(rec.Code, check.Equals, 200)
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Result, check.NotNil)

	var body map[string]any
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body["result"], check.DeepEquals, map[string]any{
		"id":         ids[0],
		"kind":       "install",
		"summary":    "install...",
		"status":     "Hold",
		"ready":      true,
		"spawn-time": "2016-04-21T01:02:03Z",
		"ready-time": "2016-04-21T01:02:03Z",
		"tasks": []any{
			map[string]any{
				"id":         ids[2],
				"kind":       "download",
				"summary":    "1...",
				"status":     "Hold",
				"log":        []any{"2016-04-21T01:02:03Z INFO l11", "2016-04-21T01:02:03Z INFO l12"},
				"progress":   map[string]any{"label": "", "done": 1., "total": 1.},
				"spawn-time": "2016-04-21T01:02:03Z",
				"ready-time": "2016-04-21T01:02:03Z",
			},
			map[string]any{
				"id":         ids[3],
				"kind":       "activate",
				"summary":    "2...",
				"status":     "Hold",
				"progress":   map[string]any{"label": "", "done": 1., "total": 1.},
				"spawn-time": "2016-04-21T01:02:03Z",
				"ready-time": "2016-04-21T01:02:03Z",
			},
		},
	})
}

func (s *generalSuite) TestStateChangeAbortIsReady(c *check.C) {
	restore := state.MockTime(time.Date(2016, 04, 21, 1, 2, 3, 0, time.UTC))
	defer restore()

	// Setup
	s.expectChangeReadAccess()
	d := s.daemon(c)
	st := d.Overlord().State()
	st.Lock()
	ids := setupChanges(st)
	st.Change(ids[0]).SetStatus(state.DoneStatus)
	st.Unlock()

	s.expectManageAccess()

	buf := bytes.NewBufferString(`{"action": "abort"}`)

	// Execute
	req, err := http.NewRequest("POST", "/v2/changes/"+ids[0], buf)
	c.Assert(err, check.IsNil)
	rspe := s.errorReq(c, req, nil, actionIsExpected)
	rec := httptest.NewRecorder()
	rspe.ServeHTTP(rec, req)

	// Verify
	c.Check(rec.Code, check.Equals, 400)
	c.Check(rspe.Status, check.Equals, 400)

	var body map[string]any
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body["result"], check.DeepEquals, map[string]any{
		"message": fmt.Sprintf("cannot abort change %s with nothing pending", ids[0]),
	})
}

func (s *generalSuite) testWarnings(c *check.C, all bool, body io.Reader) (calls string, result any) {
	s.daemon(c)

	s.expectManageAccess()

	okayWarns := func(*state.State, time.Time) int { calls += "ok"; return 0 }
	allWarns := func(*state.State) []*state.Warning { calls += "all"; return nil }
	pendingWarns := func(*state.State) ([]*state.Warning, time.Time) { calls += "show"; return nil, time.Time{} }
	restore := daemon.MockWarningsAccessors(okayWarns, allWarns, pendingWarns)
	defer restore()

	method := "GET"
	if body != nil {
		method = "POST"
	}
	q := url.Values{}
	if all {
		q.Set("select", "all")
	}
	req, err := http.NewRequest(method, "/v2/warnings?"+q.Encode(), body)
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil, actionIsExpected)

	c.Check(rsp.Status, check.Equals, 200)
	c.Assert(rsp.Result, check.NotNil)
	return calls, rsp.Result
}

func (s *generalSuite) TestAllWarnings(c *check.C) {
	calls, result := s.testWarnings(c, true, nil)
	c.Check(calls, check.Equals, "all")
	c.Check(result, check.DeepEquals, []state.Warning{})
}

func (s *generalSuite) TestSomeWarnings(c *check.C) {
	calls, result := s.testWarnings(c, false, nil)
	c.Check(calls, check.Equals, "show")
	c.Check(result, check.DeepEquals, []state.Warning{})
}

func (s *generalSuite) TestAckWarnings(c *check.C) {
	calls, result := s.testWarnings(c, false, bytes.NewReader([]byte(`{"action": "okay", "timestamp": "2006-01-02T15:04:05Z"}`)))
	c.Check(calls, check.Equals, "ok")
	c.Check(result, check.DeepEquals, 0)
}

func (s *generalSuite) TestSysInfoStorageEncHappy(c *check.C) {
	s.daemon(c)
	s.expectSystemInfoStorageEncReadAccess()

	expectedStatus := ""
	expectedResponse := map[string]any{}

	setExpectedStatus := func(status string) {
		expectedStatus = status
		expectedResponse["status"] = status
	}

	defer daemon.MockFdestateSystemEncryptedFromState(func(*state.State) (bool, error) {
		switch expectedStatus {
		case "active":
			return true, nil

		case "inactive":
			return false, nil
		}

		return false, errors.New("cannot set unsupported expected status")
	})()

	req, err := http.NewRequest("GET", "/v2/system-info/storage-encrypted", nil)
	c.Assert(err, check.IsNil)

	setExpectedStatus("active")
	rsp := s.syncReq(c, req, nil, actionIsExpected)
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Type, check.Equals, daemon.ResponseTypeSync)
	c.Check(rsp.Result, check.DeepEquals, expectedResponse)

	setExpectedStatus("inactive")
	rsp = s.syncReq(c, req, nil, actionIsExpected)
	c.Assert(err, check.IsNil)
	c.Check(rsp.Result, check.DeepEquals, expectedResponse)
}

func (s *generalSuite) TestSysInfoStorageEncErrorImpl(c *check.C) {
	s.daemon(c)

	defer daemon.MockFdestateSystemEncryptedFromState(func(*state.State) (bool, error) {
		return false, errors.New("cannot calculate status")
	})()

	req, err := http.NewRequest("GET", "/v2/system-info/storage-encrypted", nil)
	c.Assert(err, check.IsNil)

	rsp := s.errorReq(c, req, nil, actionIsExpected)
	c.Check(rsp.Status, check.Equals, 500)
	c.Check(rsp.Message, check.Equals, "cannot determine system encrypted state: cannot calculate status")
}
