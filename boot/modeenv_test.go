// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package boot_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/mvo5/goconfigparser"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/testutil"
)

// baseBootSuite is used to setup the common test environment
type modeenvSuite struct {
	testutil.BaseTest

	tmpdir          string
	mockModeenvPath string
}

var _ = Suite(&modeenvSuite{})

func (s *modeenvSuite) SetUpTest(c *C) {
	s.tmpdir = c.MkDir()
	s.mockModeenvPath = filepath.Join(s.tmpdir, dirs.SnapModeenvFile)
}

func (s *modeenvSuite) TestReadEmptyErrors(c *C) {
	modeenv, err := boot.ReadModeenv("/no/such/file")
	c.Assert(os.IsNotExist(err), Equals, true)
	c.Assert(modeenv, IsNil)
}

func (s *modeenvSuite) makeMockModeenvFile(c *C, content string) {
	err := os.MkdirAll(filepath.Dir(s.mockModeenvPath), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(s.mockModeenvPath, []byte(content), 0644)
	c.Assert(err, IsNil)
}

func (s *modeenvSuite) TestWasReadSanity(c *C) {
	modeenv := &boot.Modeenv{}
	c.Check(modeenv.WasRead(), Equals, false)
}

func (s *modeenvSuite) TestReadEmpty(c *C) {
	s.makeMockModeenvFile(c, "")

	modeenv, err := boot.ReadModeenv(s.tmpdir)
	c.Assert(err, ErrorMatches, "internal error: mode is unset")
	c.Assert(modeenv, IsNil)
}

func (s *modeenvSuite) TestReadMode(c *C) {
	s.makeMockModeenvFile(c, "mode=run")

	modeenv, err := boot.ReadModeenv(s.tmpdir)
	c.Assert(err, IsNil)
	c.Check(modeenv.Mode, Equals, "run")
	c.Check(modeenv.RecoverySystem, Equals, "")
	c.Check(modeenv.Base, Equals, "")
}

func (s *modeenvSuite) TestReadModeWithRecoverySystem(c *C) {
	s.makeMockModeenvFile(c, `mode=recovery
recovery_system=20191126
`)

	modeenv, err := boot.ReadModeenv(s.tmpdir)
	c.Assert(err, IsNil)
	c.Check(modeenv.Mode, Equals, "recovery")
	c.Check(modeenv.RecoverySystem, Equals, "20191126")
}

func (s *modeenvSuite) TestReadModeWithBase(c *C) {
	s.makeMockModeenvFile(c, `mode=recovery
recovery_system=20191126
base=core20_123.snap
try_base=core20_124.snap
base_status=try
`)

	modeenv, err := boot.ReadModeenv(s.tmpdir)
	c.Assert(err, IsNil)
	c.Check(modeenv.Mode, Equals, "recovery")
	c.Check(modeenv.RecoverySystem, Equals, "20191126")
	c.Check(modeenv.Base, Equals, "core20_123.snap")
	c.Check(modeenv.TryBase, Equals, "core20_124.snap")
	c.Check(modeenv.BaseStatus, Equals, boot.TryStatus)
}

func (s *modeenvSuite) TestReadModeWithGrade(c *C) {
	s.makeMockModeenvFile(c, `mode=run
grade=dangerous
`)
	modeenv, err := boot.ReadModeenv(s.tmpdir)
	c.Assert(err, IsNil)
	c.Check(modeenv.Mode, Equals, "run")
	c.Check(modeenv.Grade, Equals, "dangerous")

	s.makeMockModeenvFile(c, `mode=run
grade=some-random-grade-string
`)
	modeenv, err = boot.ReadModeenv(s.tmpdir)
	c.Assert(err, IsNil)
	c.Check(modeenv.Mode, Equals, "run")
	c.Check(modeenv.Grade, Equals, "some-random-grade-string")
}

func (s *modeenvSuite) TestReadModeWithModel(c *C) {
	tt := []struct {
		entry        string
		model, brand string
	}{
		{
			entry: "my-brand/my-model",
			brand: "my-brand",
			model: "my-model",
		}, {
			entry: "my-brand/",
		}, {
			entry: "my-model/",
		}, {
			entry: "foobar",
		}, {
			entry: "/",
		}, {
			entry: ",",
		}, {
			entry: "",
		},
	}

	for _, t := range tt {
		s.makeMockModeenvFile(c, `mode=run
model=`+t.entry+"\n")
		modeenv, err := boot.ReadModeenv(s.tmpdir)
		c.Assert(err, IsNil)
		c.Check(modeenv.Mode, Equals, "run")
		c.Check(modeenv.Model, Equals, t.model)
		c.Check(modeenv.BrandID, Equals, t.brand)
	}
}

func (s *modeenvSuite) TestReadModeWithCurrentKernels(c *C) {

	tt := []struct {
		kernelString    string
		expectedKernels []string
	}{
		{
			"pc-kernel_1.snap",
			[]string{"pc-kernel_1.snap"},
		},
		{
			"pc-kernel_1.snap,pc-kernel_2.snap",
			[]string{"pc-kernel_1.snap", "pc-kernel_2.snap"},
		},
		{
			"pc-kernel_1.snap,,,,,pc-kernel_2.snap",
			[]string{"pc-kernel_1.snap", "pc-kernel_2.snap"},
		},
		// we should be robust in parsing the modeenv against garbage
		{
			`pc-kernel_1.snap,this-is-not-a-real-snap$%^&^%$#@#$%^%"$,pc-kernel_2.snap`,
			[]string{"pc-kernel_1.snap", `this-is-not-a-real-snap$%^&^%$#@#$%^%"$`, "pc-kernel_2.snap"},
		},
		{",,,", nil},
		{"", nil},
	}

	for _, t := range tt {
		s.makeMockModeenvFile(c, `mode=recovery
recovery_system=20191126
current_kernels=`+t.kernelString+"\n")

		modeenv, err := boot.ReadModeenv(s.tmpdir)
		c.Assert(err, IsNil)
		c.Check(modeenv.Mode, Equals, "recovery")
		c.Check(modeenv.RecoverySystem, Equals, "20191126")
		c.Check(len(modeenv.CurrentKernels), Equals, len(t.expectedKernels))
		if len(t.expectedKernels) != 0 {
			c.Check(modeenv.CurrentKernels, DeepEquals, t.expectedKernels)
		}
	}
}

func (s *modeenvSuite) TestWriteToNonExisting(c *C) {
	c.Assert(s.mockModeenvPath, testutil.FileAbsent)

	modeenv := &boot.Modeenv{Mode: "run"}
	err := modeenv.WriteTo(s.tmpdir)
	c.Assert(err, IsNil)

	c.Assert(s.mockModeenvPath, testutil.FileEquals, "mode=run\n")
}

func (s *modeenvSuite) TestWriteToExisting(c *C) {
	s.makeMockModeenvFile(c, "mode=run")

	modeenv, err := boot.ReadModeenv(s.tmpdir)
	c.Assert(err, IsNil)
	modeenv.Mode = "recovery"
	err = modeenv.WriteTo(s.tmpdir)
	c.Assert(err, IsNil)

	c.Assert(s.mockModeenvPath, testutil.FileEquals, "mode=recovery\n")
}

func (s *modeenvSuite) TestWriteExisting(c *C) {
	s.makeMockModeenvFile(c, "mode=run")

	modeenv, err := boot.ReadModeenv(s.tmpdir)
	c.Assert(err, IsNil)
	modeenv.Mode = "recovery"
	err = modeenv.Write()
	c.Assert(err, IsNil)

	c.Assert(s.mockModeenvPath, testutil.FileEquals, "mode=recovery\n")
}

func (s *modeenvSuite) TestWriteFreshError(c *C) {
	modeenv := &boot.Modeenv{Mode: "recovery"}

	err := modeenv.Write()
	c.Assert(err, ErrorMatches, `internal error: must use WriteTo with modeenv not read from disk`)
}

func (s *modeenvSuite) TestWriteToNonExistingFull(c *C) {
	c.Assert(s.mockModeenvPath, testutil.FileAbsent)

	modeenv := &boot.Modeenv{
		Mode:                   "run",
		RecoverySystem:         "20191128",
		CurrentRecoverySystems: []string{"20191128", "2020-02-03", "20240101-FOO"},
		// keep this comment to make gofmt 1.9 happy
		Base:           "core20_321.snap",
		TryBase:        "core20_322.snap",
		BaseStatus:     boot.TryStatus,
		CurrentKernels: []string{"pc-kernel_1.snap", "pc-kernel_2.snap"},
	}
	err := modeenv.WriteTo(s.tmpdir)
	c.Assert(err, IsNil)

	c.Assert(s.mockModeenvPath, testutil.FileEquals, `mode=run
recovery_system=20191128
current_recovery_systems=20191128,2020-02-03,20240101-FOO
base=core20_321.snap
try_base=core20_322.snap
base_status=try
current_kernels=pc-kernel_1.snap,pc-kernel_2.snap
`)
}

func (s *modeenvSuite) TestReadRecoverySystems(c *C) {
	tt := []struct {
		systemsString   string
		expectedSystems []string
	}{
		{
			"20191126",
			[]string{"20191126"},
		}, {
			"20191128,2020-02-03,20240101-FOO",
			[]string{"20191128", "2020-02-03", "20240101-FOO"},
		},
		{",,,", nil},
		{"", nil},
	}

	for _, t := range tt {
		c.Logf("tc: %q", t.systemsString)
		s.makeMockModeenvFile(c, `mode=recovery
recovery_system=20191126
current_recovery_systems=`+t.systemsString+"\n")

		modeenv, err := boot.ReadModeenv(s.tmpdir)
		c.Assert(err, IsNil)
		c.Check(modeenv.Mode, Equals, "recovery")
		c.Check(modeenv.RecoverySystem, Equals, "20191126")
		c.Check(modeenv.CurrentRecoverySystems, DeepEquals, t.expectedSystems)
	}
}

type fancyDataBothMarshallers struct {
	Foo []string
}

func (f *fancyDataBothMarshallers) MarshalModeenvValue() (string, error) {
	return strings.Join(f.Foo, "#"), nil
}

func (f *fancyDataBothMarshallers) UnmarshalModeenvValue(v string) error {
	f.Foo = strings.Split(v, "#")
	return nil
}

func (f *fancyDataBothMarshallers) MarshalJSON() ([]byte, error) {
	return nil, fmt.Errorf("unexpected call to JSON marshaller")
}

func (f *fancyDataBothMarshallers) UnmarshalJSON(data []byte) error {
	return fmt.Errorf("unexpected call to JSON unmarshaller")
}

type fancyDataJSONOnly struct {
	Foo []string
}

func (f *fancyDataJSONOnly) MarshalJSON() ([]byte, error) {
	return json.Marshal(f.Foo)
}

func (f *fancyDataJSONOnly) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &f.Foo)
}

func (s *modeenvSuite) TestFancyMarshalUnmarshal(c *C) {
	var buf bytes.Buffer

	dboth := fancyDataBothMarshallers{Foo: []string{"1", "two"}}
	err := boot.MarshalNonEmptyForModeenvEntry(&buf, "fancy", &dboth)
	c.Assert(err, IsNil)
	c.Check(buf.String(), Equals, `fancy=1#two
`)

	djson := fancyDataJSONOnly{Foo: []string{"1", "two", "with\nnewline"}}
	err = boot.MarshalNonEmptyForModeenvEntry(&buf, "fancy_json", &djson)
	c.Assert(err, IsNil)
	c.Check(buf.String(), Equals, `fancy=1#two
fancy_json=["1","two","with\nnewline"]
`)

	cfg := goconfigparser.New()
	cfg.AllowNoSectionHeader = true
	err = cfg.Read(&buf)
	c.Assert(err, IsNil)

	var dbothRev fancyDataBothMarshallers
	err = boot.UnmarshalModeenvValueFromCfg(cfg, "fancy", &dbothRev)
	c.Assert(err, IsNil)
	c.Check(dbothRev, DeepEquals, dboth)

	var djsonRev fancyDataJSONOnly
	err = boot.UnmarshalModeenvValueFromCfg(cfg, "fancy_json", &djsonRev)
	c.Assert(err, IsNil)
	c.Check(djsonRev, DeepEquals, djson)
}
