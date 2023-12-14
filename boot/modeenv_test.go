// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2022 Canonical Ltd
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

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snapdenv"
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

func (s *modeenvSuite) TestKnownKnown(c *C) {
	// double check keys as found with reflect
	c.Check(boot.ModeenvKnownKeys, DeepEquals, map[string]bool{
		"mode":                     true,
		"recovery_system":          true,
		"current_recovery_systems": true,
		"good_recovery_systems":    true,
		"boot_flags":               true,
		// keep this comment to make old go fmt happy
		"base":                  true,
		"gadget":                true,
		"try_base":              true,
		"base_status":           true,
		"current_kernels":       true,
		"model":                 true,
		"classic":               true,
		"grade":                 true,
		"model_sign_key_id":     true,
		"try_model":             true,
		"try_grade":             true,
		"try_model_sign_key_id": true,
		// keep this comment to make old go fmt happy
		"current_kernel_command_lines":         true,
		"current_trusted_boot_assets":          true,
		"current_trusted_recovery_boot_assets": true,
	})
}

func (s *modeenvSuite) TestReadEmptyErrors(c *C) {
	modeenv, err := boot.ReadModeenv("/no/such/file")
	c.Assert(os.IsNotExist(err), Equals, true)
	c.Assert(modeenv, IsNil)
}

func (s *modeenvSuite) makeMockModeenvFile(c *C, content string) {
	err := os.MkdirAll(filepath.Dir(s.mockModeenvPath), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(s.mockModeenvPath, []byte(content), 0644)
	c.Assert(err, IsNil)
}

func (s *modeenvSuite) TestWasReadValidity(c *C) {
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
	c.Check(modeenv.Gadget, Equals, "")
}

func (s *modeenvSuite) TestDeepEqualDiskVsMemoryInvariant(c *C) {
	s.makeMockModeenvFile(c, `mode=recovery
recovery_system=20191126
base=core20_123.snap
gadget=pc_1.snap
try_base=core20_124.snap
base_status=try
`)

	diskModeenv, err := boot.ReadModeenv(s.tmpdir)
	c.Assert(err, IsNil)
	inMemoryModeenv := &boot.Modeenv{
		Mode:           "recovery",
		RecoverySystem: "20191126",
		Base:           "core20_123.snap",
		Gadget:         "pc_1.snap",
		TryBase:        "core20_124.snap",
		BaseStatus:     "try",
	}
	c.Assert(inMemoryModeenv.DeepEqual(diskModeenv), Equals, true)
	c.Assert(diskModeenv.DeepEqual(inMemoryModeenv), Equals, true)
}

func (s *modeenvSuite) TestCopyDeepEquals(c *C) {
	s.makeMockModeenvFile(c, `mode=recovery
recovery_system=20191126
base=core20_123.snap
try_base=core20_124.snap
base_status=try
current_trusted_boot_assets={"thing1":["hash1","hash2"],"thing2":["hash3"]}
current_kernel_command_lines=["foo", "bar"]
`)

	diskModeenv, err := boot.ReadModeenv(s.tmpdir)
	c.Assert(err, IsNil)
	inMemoryModeenv := &boot.Modeenv{
		Mode:           "recovery",
		RecoverySystem: "20191126",
		Base:           "core20_123.snap",
		TryBase:        "core20_124.snap",
		BaseStatus:     "try",
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"thing1": []string{"hash1", "hash2"},
			"thing2": []string{"hash3"},
		},
		CurrentKernelCommandLines: boot.BootCommandLines{
			"foo", "bar",
		},
	}

	c.Assert(inMemoryModeenv.DeepEqual(diskModeenv), Equals, true)
	c.Assert(diskModeenv.DeepEqual(inMemoryModeenv), Equals, true)

	diskModeenv2, err := diskModeenv.Copy()
	c.Assert(err, IsNil)
	c.Assert(diskModeenv.DeepEqual(diskModeenv2), Equals, true)
	c.Assert(diskModeenv2.DeepEqual(diskModeenv), Equals, true)
	c.Assert(inMemoryModeenv.DeepEqual(diskModeenv2), Equals, true)
	c.Assert(diskModeenv2.DeepEqual(inMemoryModeenv), Equals, true)

	inMemoryModeenv2, err := inMemoryModeenv.Copy()
	c.Assert(err, IsNil)
	c.Assert(inMemoryModeenv.DeepEqual(inMemoryModeenv2), Equals, true)
	c.Assert(inMemoryModeenv2.DeepEqual(inMemoryModeenv), Equals, true)
	c.Assert(inMemoryModeenv2.DeepEqual(diskModeenv), Equals, true)
	c.Assert(diskModeenv.DeepEqual(inMemoryModeenv2), Equals, true)
}

func (s *modeenvSuite) TestCopyDiskWriteWorks(c *C) {
	s.makeMockModeenvFile(c, `mode=recovery
recovery_system=20191126
base=core20_123.snap
try_base=core20_124.snap
base_status=try
`)

	diskModeenv, err := boot.ReadModeenv(s.tmpdir)
	c.Assert(err, IsNil)
	dupDiskModeenv, err := diskModeenv.Copy()
	c.Assert(err, IsNil)

	// move the original file out of the way
	err = os.Rename(dirs.SnapModeenvFileUnder(s.tmpdir), dirs.SnapModeenvFileUnder(s.tmpdir)+".orig")
	c.Assert(err, IsNil)
	c.Assert(dirs.SnapModeenvFileUnder(s.tmpdir), testutil.FileAbsent)

	// write the duplicate, it should write to the same original location and it
	// should be the same content
	err = dupDiskModeenv.Write()
	c.Assert(err, IsNil)
	c.Assert(dirs.SnapModeenvFileUnder(s.tmpdir), testutil.FilePresent)
	origBytes, err := ioutil.ReadFile(dirs.SnapModeenvFileUnder(s.tmpdir) + ".orig")
	c.Assert(err, IsNil)
	// the files should be the same
	c.Assert(dirs.SnapModeenvFileUnder(s.tmpdir), testutil.FileEquals, string(origBytes))
}

func (s *modeenvSuite) TestCopyMemoryWriteFails(c *C) {
	inMemoryModeenv := &boot.Modeenv{
		Mode:           "recovery",
		RecoverySystem: "20191126",
		Base:           "core20_123.snap",
		TryBase:        "core20_124.snap",
		BaseStatus:     "try",
	}
	dupInMemoryModeenv, err := inMemoryModeenv.Copy()
	c.Assert(err, IsNil)

	// write the duplicate, it should fail
	err = dupInMemoryModeenv.Write()
	c.Assert(err, ErrorMatches, "internal error: must use WriteTo with modeenv not read from disk")
}

func (s *modeenvSuite) TestDeepEquals(c *C) {
	// start with two identical modeenvs
	modeenv1 := &boot.Modeenv{
		Mode:                   "recovery",
		RecoverySystem:         "20191126",
		CurrentRecoverySystems: []string{"1", "2"},
		GoodRecoverySystems:    []string{"3"},

		Base:           "core20_123.snap",
		TryBase:        "core20_124.snap",
		BaseStatus:     "try",
		CurrentKernels: []string{"k1", "k2"},

		Model:          "model",
		BrandID:        "brand",
		Grade:          "secured",
		ModelSignKeyID: "9tydnLa6MTJ-jaQTFUXEwHl1yRx7ZS4K5cyFDhYDcPzhS7uyEkDxdUjg9g08BtNn",

		BootFlags: []string{"foo", "factory"},

		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"thing1": []string{"hash1", "hash2"},
			"thing2": []string{"hash3"},
		},

		CurrentKernelCommandLines: boot.BootCommandLines{
			"foo",
			"foo bar",
		},
	}

	modeenv2 := &boot.Modeenv{
		Mode:                   "recovery",
		RecoverySystem:         "20191126",
		CurrentRecoverySystems: []string{"1", "2"},
		GoodRecoverySystems:    []string{"3"},

		Base:           "core20_123.snap",
		TryBase:        "core20_124.snap",
		BaseStatus:     "try",
		CurrentKernels: []string{"k1", "k2"},

		Model:          "model",
		BrandID:        "brand",
		Grade:          "secured",
		ModelSignKeyID: "9tydnLa6MTJ-jaQTFUXEwHl1yRx7ZS4K5cyFDhYDcPzhS7uyEkDxdUjg9g08BtNn",

		BootFlags: []string{"foo", "factory"},

		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"thing1": []string{"hash1", "hash2"},
			"thing2": []string{"hash3"},
		},

		CurrentKernelCommandLines: boot.BootCommandLines{
			"foo",
			"foo bar",
		},
	}

	// same object should be the same
	c.Assert(modeenv1.DeepEqual(modeenv1), Equals, true)

	// no difference should be the same at the start
	c.Assert(modeenv1.DeepEqual(modeenv2), Equals, true)
	c.Assert(modeenv2.DeepEqual(modeenv1), Equals, true)

	// invert CurrentKernels
	modeenv2.CurrentKernels = []string{"k2", "k1"}
	c.Assert(modeenv1.DeepEqual(modeenv2), Equals, false)
	c.Assert(modeenv2.DeepEqual(modeenv1), Equals, false)

	// make CurrentKernels capitalized
	modeenv2.CurrentKernels = []string{"K1", "k2"}
	c.Assert(modeenv1.DeepEqual(modeenv2), Equals, false)
	c.Assert(modeenv2.DeepEqual(modeenv1), Equals, false)

	// make CurrentKernels disappear
	modeenv2.CurrentKernels = nil
	c.Assert(modeenv1.DeepEqual(modeenv2), Equals, false)
	c.Assert(modeenv2.DeepEqual(modeenv1), Equals, false)

	// make it identical again
	modeenv2.CurrentKernels = []string{"k1", "k2"}
	c.Assert(modeenv1.DeepEqual(modeenv2), Equals, true)
	// change kernel command lines
	modeenv2.CurrentKernelCommandLines = boot.BootCommandLines{
		// reversed order
		"foo bar",
		"foo",
	}
	c.Assert(modeenv1.DeepEqual(modeenv2), Equals, false)
	// clear kernel command lines list
	modeenv2.CurrentKernelCommandLines = nil
	c.Assert(modeenv1.DeepEqual(modeenv2), Equals, false)

	// make it identical again
	modeenv2.CurrentKernelCommandLines = boot.BootCommandLines{
		"foo",
		"foo bar",
	}
	c.Assert(modeenv1.DeepEqual(modeenv2), Equals, true)

	// change the list of current recovery systems
	modeenv2.CurrentRecoverySystems = append(modeenv2.CurrentRecoverySystems, "1234")
	c.Assert(modeenv1.DeepEqual(modeenv2), Equals, false)
	// make it identical again
	modeenv2.CurrentRecoverySystems = []string{"1", "2"}
	c.Assert(modeenv1.DeepEqual(modeenv2), Equals, true)

	// change the list of good recovery systems
	modeenv2.GoodRecoverySystems = append(modeenv2.GoodRecoverySystems, "999")
	c.Assert(modeenv1.DeepEqual(modeenv2), Equals, false)
	// restore it
	modeenv2.GoodRecoverySystems = modeenv2.GoodRecoverySystems[:len(modeenv2.GoodRecoverySystems)-1]

	// change the sign key ID
	modeenv2.ModelSignKeyID = "EAD4DbLxK_kn0gzNCXOs3kd6DeMU3f-L6BEsSEuJGBqCORR0gXkdDxMbOm11mRFu"
	c.Assert(modeenv1.DeepEqual(modeenv2), Equals, false)
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

func (s *modeenvSuite) TestReadModeenvWithUnknownKeysKeepsWrites(c *C) {
	s.makeMockModeenvFile(c, `first_unknown=thing
mode=recovery
recovery_system=20191126
unknown_key=some unknown value
a_key=other
`)

	modeenv, err := boot.ReadModeenv(s.tmpdir)
	c.Assert(err, IsNil)
	c.Check(modeenv.Mode, Equals, "recovery")
	c.Check(modeenv.RecoverySystem, Equals, "20191126")

	c.Assert(modeenv.Write(), IsNil)

	c.Assert(s.mockModeenvPath, testutil.FileEquals, `mode=recovery
recovery_system=20191126
a_key=other
first_unknown=thing
unknown_key=some unknown value
`)
}

func (s *modeenvSuite) TestReadModeenvWithUnknownKeysDeepEqualsSameWithoutUnknownKeys(c *C) {
	s.makeMockModeenvFile(c, `first_unknown=thing
mode=recovery
recovery_system=20191126
try_base=core20_124.snap
base_status=try
unknown_key=some unknown value
current_trusted_boot_assets={"grubx64.efi":["hash1","hash2"]}
current_trusted_recovery_boot_assets={"bootx64.efi":["shimhash1","shimhash2"],"grubx64.efi":["recovery-hash1"]}
a_key=other
`)

	modeenvWithExtraKeys, err := boot.ReadModeenv(s.tmpdir)
	c.Assert(err, IsNil)
	c.Check(modeenvWithExtraKeys.Mode, Equals, "recovery")
	c.Check(modeenvWithExtraKeys.RecoverySystem, Equals, "20191126")

	// should be the same as one that with just those keys in memory
	c.Assert(modeenvWithExtraKeys.DeepEqual(&boot.Modeenv{
		Mode:           "recovery",
		RecoverySystem: "20191126",
		TryBase:        "core20_124.snap",
		BaseStatus:     boot.TryStatus,
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"grubx64.efi": []string{"hash1", "hash2"},
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"bootx64.efi": []string{"shimhash1", "shimhash2"},
			"grubx64.efi": []string{"recovery-hash1"},
		},
	}), Equals, true)
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

func (s *modeenvSuite) TestWriteIncompleteModelBrand(c *C) {
	modeenv := &boot.Modeenv{
		Mode:  "run",
		Grade: "dangerous",
	}

	err := modeenv.WriteTo(s.tmpdir)
	c.Assert(err, ErrorMatches, `internal error: model is unset`)

	modeenv.Model = "bar"
	err = modeenv.WriteTo(s.tmpdir)
	c.Assert(err, ErrorMatches, `internal error: brand is unset`)

	modeenv.BrandID = "foo"
	modeenv.TryGrade = "dangerous"
	err = modeenv.WriteTo(s.tmpdir)
	c.Assert(err, ErrorMatches, `internal error: try model is unset`)

	modeenv.TryModel = "bar"
	err = modeenv.WriteTo(s.tmpdir)
	c.Assert(err, ErrorMatches, `internal error: try brand is unset`)

	modeenv.TryBrandID = "foo"
	err = modeenv.WriteTo(s.tmpdir)
	c.Assert(err, IsNil)
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
		s.makeMockModeenvFile(c, fmt.Sprintf(`mode=recovery
recovery_system=20191126
current_recovery_systems=%[1]s
good_recovery_systems=%[1]s
`, t.systemsString))

		modeenv, err := boot.ReadModeenv(s.tmpdir)
		c.Assert(err, IsNil)
		c.Check(modeenv.Mode, Equals, "recovery")
		c.Check(modeenv.RecoverySystem, Equals, "20191126")
		c.Check(modeenv.CurrentRecoverySystems, DeepEquals, t.expectedSystems)
		c.Check(modeenv.GoodRecoverySystems, DeepEquals, t.expectedSystems)
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
	err := boot.MarshalModeenvEntryTo(&buf, "fancy", &dboth)
	c.Assert(err, IsNil)
	c.Check(buf.String(), Equals, `fancy=1#two
`)

	djson := fancyDataJSONOnly{Foo: []string{"1", "two", "with\nnewline"}}
	err = boot.MarshalModeenvEntryTo(&buf, "fancy_json", &djson)
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

func (s *modeenvSuite) TestFancyUnmarshalJSONEmpty(c *C) {
	var buf bytes.Buffer

	cfg := goconfigparser.New()
	cfg.AllowNoSectionHeader = true
	err := cfg.Read(&buf)
	c.Assert(err, IsNil)

	var djsonRev fancyDataJSONOnly
	err = boot.UnmarshalModeenvValueFromCfg(cfg, "fancy_json", &djsonRev)
	c.Assert(err, IsNil)
	c.Check(djsonRev.Foo, IsNil)
}

func (s *modeenvSuite) TestMarshalCurrentTrustedBootAssets(c *C) {
	c.Assert(s.mockModeenvPath, testutil.FileAbsent)

	modeenv := &boot.Modeenv{
		Mode:           "run",
		RecoverySystem: "20191128",
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"grubx64.efi": []string{"hash1", "hash2"},
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"grubx64.efi": []string{"recovery-hash1"},
			"bootx64.efi": []string{"shimhash1", "shimhash2"},
		},
	}
	err := modeenv.WriteTo(s.tmpdir)
	c.Assert(err, IsNil)

	c.Assert(s.mockModeenvPath, testutil.FileEquals, `mode=run
recovery_system=20191128
current_trusted_boot_assets={"grubx64.efi":["hash1","hash2"]}
current_trusted_recovery_boot_assets={"bootx64.efi":["shimhash1","shimhash2"],"grubx64.efi":["recovery-hash1"]}
`)

	modeenvRead, err := boot.ReadModeenv(s.tmpdir)
	c.Assert(err, IsNil)
	c.Assert(modeenvRead.CurrentTrustedBootAssets, DeepEquals, boot.BootAssetsMap{
		"grubx64.efi": []string{"hash1", "hash2"},
	})
	c.Assert(modeenvRead.CurrentTrustedRecoveryBootAssets, DeepEquals, boot.BootAssetsMap{
		"grubx64.efi": []string{"recovery-hash1"},
		"bootx64.efi": []string{"shimhash1", "shimhash2"},
	})
}

func (s *modeenvSuite) TestMarshalKernelCommandLines(c *C) {
	c.Assert(s.mockModeenvPath, testutil.FileAbsent)

	modeenv := &boot.Modeenv{
		Mode:           "run",
		RecoverySystem: "20191128",
		CurrentKernelCommandLines: boot.BootCommandLines{
			`snapd_recovery_mode=run panic=-1 console=ttyS0,io,9600n8`,
			`snapd_recovery_mode=run candidate panic=-1 console=ttyS0,io,9600n8`,
		},
	}
	err := modeenv.WriteTo(s.tmpdir)
	c.Assert(err, IsNil)

	c.Assert(s.mockModeenvPath, testutil.FileEquals, `mode=run
recovery_system=20191128
current_kernel_command_lines=["snapd_recovery_mode=run panic=-1 console=ttyS0,io,9600n8","snapd_recovery_mode=run candidate panic=-1 console=ttyS0,io,9600n8"]
`)

	modeenvRead, err := boot.ReadModeenv(s.tmpdir)
	c.Assert(err, IsNil)
	c.Assert(modeenvRead.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		`snapd_recovery_mode=run panic=-1 console=ttyS0,io,9600n8`,
		`snapd_recovery_mode=run candidate panic=-1 console=ttyS0,io,9600n8`,
	})
}

func (s *modeenvSuite) TestModeenvWithModelGradeSignKeyID(c *C) {
	s.makeMockModeenvFile(c, `mode=run
model=canonical/ubuntu-core-20-amd64
grade=dangerous
model_sign_key_id=9tydnLa6MTJ-jaQTFUXEwHl1yRx7ZS4K5cyFDhYDcPzhS7uyEkDxdUjg9g08BtNn
try_model=developer1/testkeys-snapd-secured-core-20-amd64
try_grade=secured
try_model_sign_key_id=EAD4DbLxK_kn0gzNCXOs3kd6DeMU3f-L6BEsSEuJGBqCORR0gXkdDxMbOm11mRFu
`)

	modeenv, err := boot.ReadModeenv(s.tmpdir)
	c.Assert(err, IsNil)
	c.Check(modeenv.Model, Equals, "ubuntu-core-20-amd64")
	c.Check(modeenv.BrandID, Equals, "canonical")
	c.Check(modeenv.Classic, Equals, false)
	c.Check(modeenv.Grade, Equals, "dangerous")
	c.Check(modeenv.ModelSignKeyID, Equals, "9tydnLa6MTJ-jaQTFUXEwHl1yRx7ZS4K5cyFDhYDcPzhS7uyEkDxdUjg9g08BtNn")
	// candidate model
	c.Check(modeenv.TryModel, Equals, "testkeys-snapd-secured-core-20-amd64")
	c.Check(modeenv.TryBrandID, Equals, "developer1")
	c.Check(modeenv.TryGrade, Equals, "secured")
	c.Check(modeenv.TryModelSignKeyID, Equals, "EAD4DbLxK_kn0gzNCXOs3kd6DeMU3f-L6BEsSEuJGBqCORR0gXkdDxMbOm11mRFu")

	// change some model data now
	modeenv.Model = "testkeys-snapd-signed-core-20-amd64"
	modeenv.BrandID = "developer1"
	modeenv.Grade = "signed"
	modeenv.ModelSignKeyID = "EAD4DbLxK_kn0gzNCXOs3kd6DeMU3f-L6BEsSEuJGBqCORR0gXkdDxMbOm11mRFu"

	modeenv.TryModel = "bar"
	modeenv.TryBrandID = "foo"
	modeenv.TryGrade = "dangerous"
	modeenv.TryModelSignKeyID = "9tydnLa6MTJ-jaQTFUXEwHl1yRx7ZS4K5cyFDhYDcPzhS7uyEkDxdUjg9g08BtNn"

	// and write it
	c.Assert(modeenv.Write(), IsNil)

	c.Assert(s.mockModeenvPath, testutil.FileEquals, `mode=run
model=developer1/testkeys-snapd-signed-core-20-amd64
grade=signed
model_sign_key_id=EAD4DbLxK_kn0gzNCXOs3kd6DeMU3f-L6BEsSEuJGBqCORR0gXkdDxMbOm11mRFu
try_model=foo/bar
try_grade=dangerous
try_model_sign_key_id=9tydnLa6MTJ-jaQTFUXEwHl1yRx7ZS4K5cyFDhYDcPzhS7uyEkDxdUjg9g08BtNn
`)
}

func (s *modeenvSuite) TestModeenvWithClassicModelGradeSignKeyID(c *C) {
	s.makeMockModeenvFile(c, `mode=run
model=canonical/ubuntu-classic-20-amd64
grade=dangerous
classic=true
model_sign_key_id=9tydnLa6MTJ-jaQTFUXEwHl1yRx7ZS4K5cyFDhYDcPzhS7uyEkDxdUjg9g08BtNn
try_model=developer1/testkeys-snapd-secured-classic-20-amd64
try_grade=secured
try_model_sign_key_id=EAD4DbLxK_kn0gzNCXOs3kd6DeMU3f-L6BEsSEuJGBqCORR0gXkdDxMbOm11mRFu
`)

	modeenv, err := boot.ReadModeenv(s.tmpdir)
	c.Assert(err, IsNil)
	c.Check(modeenv.Model, Equals, "ubuntu-classic-20-amd64")
	c.Check(modeenv.BrandID, Equals, "canonical")
	c.Check(modeenv.Classic, Equals, true)
	c.Check(modeenv.Grade, Equals, "dangerous")
	c.Check(modeenv.ModelSignKeyID, Equals, "9tydnLa6MTJ-jaQTFUXEwHl1yRx7ZS4K5cyFDhYDcPzhS7uyEkDxdUjg9g08BtNn")
	// candidate model
	c.Check(modeenv.TryModel, Equals, "testkeys-snapd-secured-classic-20-amd64")
	c.Check(modeenv.TryBrandID, Equals, "developer1")
	c.Check(modeenv.TryGrade, Equals, "secured")
	c.Check(modeenv.TryModelSignKeyID, Equals, "EAD4DbLxK_kn0gzNCXOs3kd6DeMU3f-L6BEsSEuJGBqCORR0gXkdDxMbOm11mRFu")

	// change some model data now
	modeenv.Model = "testkeys-snapd-signed-classic-20-amd64"
	modeenv.BrandID = "developer1"
	modeenv.Grade = "signed"
	modeenv.ModelSignKeyID = "EAD4DbLxK_kn0gzNCXOs3kd6DeMU3f-L6BEsSEuJGBqCORR0gXkdDxMbOm11mRFu"

	modeenv.TryModel = "bar"
	modeenv.TryBrandID = "foo"
	modeenv.TryGrade = "dangerous"
	modeenv.TryModelSignKeyID = "9tydnLa6MTJ-jaQTFUXEwHl1yRx7ZS4K5cyFDhYDcPzhS7uyEkDxdUjg9g08BtNn"

	// and write it
	c.Assert(modeenv.Write(), IsNil)

	c.Assert(s.mockModeenvPath, testutil.FileEquals, `mode=run
model=developer1/testkeys-snapd-signed-classic-20-amd64
classic=true
grade=signed
model_sign_key_id=EAD4DbLxK_kn0gzNCXOs3kd6DeMU3f-L6BEsSEuJGBqCORR0gXkdDxMbOm11mRFu
try_model=foo/bar
try_grade=dangerous
try_model_sign_key_id=9tydnLa6MTJ-jaQTFUXEwHl1yRx7ZS4K5cyFDhYDcPzhS7uyEkDxdUjg9g08BtNn
`)
}

func (s *modeenvSuite) TestModelForSealing(c *C) {
	s.makeMockModeenvFile(c, `mode=run
model=canonical/ubuntu-core-20-amd64
grade=dangerous
model_sign_key_id=9tydnLa6MTJ-jaQTFUXEwHl1yRx7ZS4K5cyFDhYDcPzhS7uyEkDxdUjg9g08BtNn
try_model=developer1/testkeys-snapd-secured-core-20-amd64
try_grade=secured
try_model_sign_key_id=EAD4DbLxK_kn0gzNCXOs3kd6DeMU3f-L6BEsSEuJGBqCORR0gXkdDxMbOm11mRFu
`)

	modeenv, err := boot.ReadModeenv(s.tmpdir)
	c.Assert(err, IsNil)

	modelForSealing := modeenv.ModelForSealing()
	c.Check(modelForSealing.Model(), Equals, "ubuntu-core-20-amd64")
	c.Check(modelForSealing.BrandID(), Equals, "canonical")
	c.Check(modelForSealing.Classic(), Equals, false)
	c.Check(modelForSealing.Grade(), Equals, asserts.ModelGrade("dangerous"))
	c.Check(modelForSealing.SignKeyID(), Equals, "9tydnLa6MTJ-jaQTFUXEwHl1yRx7ZS4K5cyFDhYDcPzhS7uyEkDxdUjg9g08BtNn")
	c.Check(modelForSealing.Series(), Equals, "16")
	c.Check(boot.ModelUniqueID(modelForSealing), Equals,
		"canonical/ubuntu-core-20-amd64,dangerous,9tydnLa6MTJ-jaQTFUXEwHl1yRx7ZS4K5cyFDhYDcPzhS7uyEkDxdUjg9g08BtNn")

	tryModelForSealing := modeenv.TryModelForSealing()
	c.Check(tryModelForSealing.Model(), Equals, "testkeys-snapd-secured-core-20-amd64")
	c.Check(tryModelForSealing.BrandID(), Equals, "developer1")
	c.Check(tryModelForSealing.Classic(), Equals, false)
	c.Check(tryModelForSealing.Grade(), Equals, asserts.ModelGrade("secured"))
	c.Check(tryModelForSealing.SignKeyID(), Equals, "EAD4DbLxK_kn0gzNCXOs3kd6DeMU3f-L6BEsSEuJGBqCORR0gXkdDxMbOm11mRFu")
	c.Check(tryModelForSealing.Series(), Equals, "16")
	c.Check(boot.ModelUniqueID(tryModelForSealing), Equals,
		"developer1/testkeys-snapd-secured-core-20-amd64,secured,EAD4DbLxK_kn0gzNCXOs3kd6DeMU3f-L6BEsSEuJGBqCORR0gXkdDxMbOm11mRFu")
}

func (s *modeenvSuite) TestClassicModelForSealing(c *C) {
	s.makeMockModeenvFile(c, `mode=run
model=canonical/ubuntu-core-20-amd64
classic=true
grade=dangerous
model_sign_key_id=9tydnLa6MTJ-jaQTFUXEwHl1yRx7ZS4K5cyFDhYDcPzhS7uyEkDxdUjg9g08BtNn
try_model=developer1/testkeys-snapd-secured-core-20-amd64
try_grade=secured
try_model_sign_key_id=EAD4DbLxK_kn0gzNCXOs3kd6DeMU3f-L6BEsSEuJGBqCORR0gXkdDxMbOm11mRFu
`)

	modeenv, err := boot.ReadModeenv(s.tmpdir)
	c.Assert(err, IsNil)

	modelForSealing := modeenv.ModelForSealing()
	c.Check(modelForSealing.Model(), Equals, "ubuntu-core-20-amd64")
	c.Check(modelForSealing.BrandID(), Equals, "canonical")
	c.Check(modelForSealing.Classic(), Equals, true)
	c.Check(modelForSealing.Grade(), Equals, asserts.ModelGrade("dangerous"))
	c.Check(modelForSealing.SignKeyID(), Equals, "9tydnLa6MTJ-jaQTFUXEwHl1yRx7ZS4K5cyFDhYDcPzhS7uyEkDxdUjg9g08BtNn")
	c.Check(modelForSealing.Series(), Equals, "16")
	c.Check(boot.ModelUniqueID(modelForSealing), Equals,
		"canonical/ubuntu-core-20-amd64,dangerous,9tydnLa6MTJ-jaQTFUXEwHl1yRx7ZS4K5cyFDhYDcPzhS7uyEkDxdUjg9g08BtNn")

	tryModelForSealing := modeenv.TryModelForSealing()
	c.Check(tryModelForSealing.Model(), Equals, "testkeys-snapd-secured-core-20-amd64")
	c.Check(tryModelForSealing.BrandID(), Equals, "developer1")
	c.Check(tryModelForSealing.Classic(), Equals, true)
	c.Check(tryModelForSealing.Grade(), Equals, asserts.ModelGrade("secured"))
	c.Check(tryModelForSealing.SignKeyID(), Equals, "EAD4DbLxK_kn0gzNCXOs3kd6DeMU3f-L6BEsSEuJGBqCORR0gXkdDxMbOm11mRFu")
	c.Check(tryModelForSealing.Series(), Equals, "16")
	c.Check(boot.ModelUniqueID(tryModelForSealing), Equals,
		"developer1/testkeys-snapd-secured-core-20-amd64,secured,EAD4DbLxK_kn0gzNCXOs3kd6DeMU3f-L6BEsSEuJGBqCORR0gXkdDxMbOm11mRFu")
}

func (s *modeenvSuite) TestModeenvAccessFailsDuringPreseeding(c *C) {
	restore := snapdenv.MockPreseeding(true)
	defer restore()

	_, err := boot.ReadModeenv(s.tmpdir)
	c.Assert(err, ErrorMatches, `internal error: modeenv cannot be read during preseeding`)

	var modeenv boot.Modeenv
	err = modeenv.WriteTo(s.tmpdir)
	c.Assert(err, ErrorMatches, `internal error: modeenv cannot be written during preseeding`)
}
