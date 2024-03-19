// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2023 Canonical Ltd
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

package asserts_test

import (
	"fmt"
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/testutil"
)

type modelSuite struct {
	ts     time.Time
	tsLine string
}

var (
	_ = Suite(&modelSuite{})
)

func (mods *modelSuite) SetUpSuite(c *C) {
	mods.ts = time.Now().Truncate(time.Second).UTC()
	mods.tsLine = "timestamp: " + mods.ts.Format(time.RFC3339) + "\n"
}

const (
	reqSnaps     = "required-snaps:\n  - foo\n  - bar\n"
	sysUserAuths = "system-user-authority: *\n"
	serialAuths  = "serial-authority:\n  - generic\n"
	preseedAuths = "preseed-authority:\n  - preseed-delegate\n"
)

const (
	modelExample = "type: model\n" +
		"authority-id: brand-id1\n" +
		"series: 16\n" +
		"brand-id: brand-id1\n" +
		"model: baz-3000\n" +
		"display-name: Baz 3000\n" +
		"architecture: amd64\n" +
		"gadget: brand-gadget\n" +
		"base: core18\n" +
		"kernel: baz-linux\n" +
		"store: brand-store\n" +
		serialAuths +
		sysUserAuths +
		reqSnaps +
		preseedAuths +
		"TSLINE" +
		"body-length: 0\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="

	classicModelExample = "type: model\n" +
		"authority-id: brand-id1\n" +
		"series: 16\n" +
		"brand-id: brand-id1\n" +
		"model: baz-3000\n" +
		"display-name: Baz 3000\n" +
		"classic: true\n" +
		"architecture: amd64\n" +
		"gadget: brand-gadget\n" +
		"store: brand-store\n" +
		reqSnaps +
		"TSLINE" +
		"body-length: 0\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="

	core20ModelExample = `type: model
authority-id: brand-id1
series: 16
brand-id: brand-id1
model: baz-3000
display-name: Baz 3000
architecture: amd64
system-user-authority: *
base: core20
store: brand-store
snaps:
  -
    name: baz-linux
    id: bazlinuxidididididididididididid
    type: kernel
    default-channel: 20
  -
    name: brand-gadget
    id: brandgadgetdidididididididididid
    type: gadget
  -
    name: other-base
    id: otherbasedididididididididididid
    type: base
    modes:
      - run
    presence: required
  -
    name: nm
    id: nmididididididididididididididid
    modes:
      - ephemeral
      - run
    default-channel: 1.0
  -
    name: myapp
    id: myappdididididididididididididid
    type: app
    default-channel: 2.0
  -
    name: myappopt
    id: myappoptidididididididididididid
    type: app
    presence: optional
OTHERgrade: secured
storage-safety: encrypted
` + "TSLINE" +
		"body-length: 0\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="

	classicModelWithSnapsExample = `type: model
authority-id: brand-id1
series: 16
brand-id: brand-id1
model: baz-3000
display-name: Baz 3000
architecture: amd64
system-user-authority: *
base: core20
classic: true
distribution: ubuntu
store: brand-store
snaps:
  -
    name: baz-linux
    id: bazlinuxidididididididididididid
    type: kernel
    default-channel: 20
  -
    name: brand-gadget
    id: brandgadgetdidididididididididid
    type: gadget
  -
    name: other-base
    id: otherbasedididididididididididid
    type: base
    modes:
      - run
    presence: required
  -
    name: nm
    id: nmididididididididididididididid
    modes:
      - ephemeral
      - run
    default-channel: 1.0
  -
    name: myapp
    id: myappdididididididididididididid
    type: app
    default-channel: 2.0
  -
    name: myappopt
    id: myappoptidididididididididididid
    type: app
    presence: optional
    classic: true
OTHERgrade: secured
storage-safety: encrypted
` + "TSLINE" +
		"body-length: 0\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="

	coreModelWithComponentsExample = `type: model
authority-id: brand-id1
series: 16
brand-id: brand-id1
model: baz-3000
display-name: Baz 3000
architecture: amd64
system-user-authority: *
base: core24
store: brand-store
snaps:
  -
    name: baz-linux
    id: bazlinuxidididididididididididid
    type: kernel
    default-channel: 20
  -
    name: brand-gadget
    id: brandgadgetdidididididididididid
    type: gadget
  -
    name: other-base
    id: otherbasedididididididididididid
    type: base
    presence: required
  -
    name: nm
    id: nmididididididididididididididid
    modes:
      - ephemeral
      - run
    default-channel: 1.0
    components:
      comp1:
        presence: optional
        modes:
          - ephemeral
      comp2: required
  -
    name: myapp
    id: myappdididididididididididididid
    type: app
    default-channel: 2.0
    presence: optional
    modes:
      - ephemeral
      - run
    components:
      comp1:
        presence: optional
        modes:
          - ephemeral
          - run
      comp2: required
  -
    name: myappopt
    id: myappoptidididididididididididid
    type: app
    presence: required
    components:
      comp1:
        presence: optional
      comp2: required
OTHERgrade: secured
storage-safety: encrypted
` + "TSLINE" +
		"body-length: 0\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="
)

func (mods *modelSuite) TestDecodeOK(c *C) {
	encoded := strings.Replace(modelExample, "TSLINE", mods.tsLine, 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.ModelType)
	model := a.(*asserts.Model)
	c.Check(model.AuthorityID(), Equals, "brand-id1")
	c.Check(model.Timestamp(), Equals, mods.ts)
	c.Check(model.Series(), Equals, "16")
	c.Check(model.BrandID(), Equals, "brand-id1")
	c.Check(model.Model(), Equals, "baz-3000")
	c.Check(model.DisplayName(), Equals, "Baz 3000")
	c.Check(model.Architecture(), Equals, "amd64")
	c.Check(model.GadgetSnap(), DeepEquals, &asserts.ModelSnap{
		Name:     "brand-gadget",
		SnapType: "gadget",
		Modes:    []string{"run"},
		Presence: "required",
	})
	c.Check(model.Gadget(), Equals, "brand-gadget")
	c.Check(model.GadgetTrack(), Equals, "")
	c.Check(model.KernelSnap(), DeepEquals, &asserts.ModelSnap{
		Name:     "baz-linux",
		SnapType: "kernel",
		Modes:    []string{"run"},
		Presence: "required",
	})
	c.Check(model.Kernel(), Equals, "baz-linux")
	c.Check(model.KernelTrack(), Equals, "")
	c.Check(model.Base(), Equals, "core18")
	c.Check(model.BaseSnap(), DeepEquals, &asserts.ModelSnap{
		Name:     "core18",
		SnapType: "base",
		Modes:    []string{"run"},
		Presence: "required",
	})
	c.Check(model.Store(), Equals, "brand-store")
	c.Check(model.Grade(), Equals, asserts.ModelGradeUnset)
	c.Check(model.StorageSafety(), Equals, asserts.StorageSafetyUnset)
	essentialSnaps := model.EssentialSnaps()
	c.Check(essentialSnaps, DeepEquals, []*asserts.ModelSnap{
		model.KernelSnap(),
		model.BaseSnap(),
		model.GadgetSnap(),
	})
	snaps := model.SnapsWithoutEssential()
	c.Check(snaps, DeepEquals, []*asserts.ModelSnap{
		{
			Name:     "foo",
			Modes:    []string{"run"},
			Presence: "required",
		},
		{
			Name:     "bar",
			Modes:    []string{"run"},
			Presence: "required",
		},
	})
	// essential snaps included
	reqSnaps := naming.NewSnapSet(model.RequiredWithEssentialSnaps())
	for _, e := range essentialSnaps {
		c.Check(reqSnaps.Contains(e), Equals, true)
	}
	for _, s := range snaps {
		c.Check(reqSnaps.Contains(s), Equals, true)
	}
	c.Check(reqSnaps.Size(), Equals, len(essentialSnaps)+len(snaps))
	// essential snaps excluded
	noEssential := naming.NewSnapSet(model.RequiredNoEssentialSnaps())
	for _, e := range essentialSnaps {
		c.Check(noEssential.Contains(e), Equals, false)
	}
	for _, s := range snaps {
		c.Check(noEssential.Contains(s), Equals, true)
	}
	c.Check(noEssential.Size(), Equals, len(snaps))

	c.Check(model.SystemUserAuthority(), HasLen, 0)
	c.Check(model.SerialAuthority(), DeepEquals, []string{"brand-id1", "generic"})
	c.Check(model.PreseedAuthority(), DeepEquals, []string{"brand-id1", "preseed-delegate"})
}

func (mods *modelSuite) TestDecodeStoreIsOptional(c *C) {
	withTimestamp := strings.Replace(modelExample, "TSLINE", mods.tsLine, 1)
	encoded := strings.Replace(withTimestamp, "store: brand-store\n", "store: \n", 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	model := a.(*asserts.Model)
	c.Check(model.Store(), Equals, "")

	encoded = strings.Replace(withTimestamp, "store: brand-store\n", "", 1)
	a, err = asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	model = a.(*asserts.Model)
	c.Check(model.Store(), Equals, "")
}

func (mods *modelSuite) TestDecodeBaseIsOptional(c *C) {
	withTimestamp := strings.Replace(modelExample, "TSLINE", mods.tsLine, 1)
	encoded := strings.Replace(withTimestamp, "base: core18\n", "base: \n", 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	model := a.(*asserts.Model)
	c.Check(model.Base(), Equals, "")

	encoded = strings.Replace(withTimestamp, "base: core18\n", "", 1)
	a, err = asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	model = a.(*asserts.Model)
	c.Check(model.Base(), Equals, "")
	c.Check(model.BaseSnap(), IsNil)
}

func (mods *modelSuite) TestDecodeDisplayNameIsOptional(c *C) {
	withTimestamp := strings.Replace(modelExample, "TSLINE", mods.tsLine, 1)
	encoded := strings.Replace(withTimestamp, "display-name: Baz 3000\n", "display-name: \n", 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	model := a.(*asserts.Model)
	// optional but we fallback to Model
	c.Check(model.DisplayName(), Equals, "baz-3000")

	encoded = strings.Replace(withTimestamp, "display-name: Baz 3000\n", "", 1)
	a, err = asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	model = a.(*asserts.Model)
	// optional but we fallback to Model
	c.Check(model.DisplayName(), Equals, "baz-3000")
}

func (mods *modelSuite) TestDecodeRequiredSnapsAreOptional(c *C) {
	withTimestamp := strings.Replace(modelExample, "TSLINE", mods.tsLine, 1)
	encoded := strings.Replace(withTimestamp, reqSnaps, "", 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	model := a.(*asserts.Model)
	c.Check(model.RequiredNoEssentialSnaps(), HasLen, 0)
}

func (mods *modelSuite) TestDecodeValidatesSnapNames(c *C) {
	withTimestamp := strings.Replace(modelExample, "TSLINE", mods.tsLine, 1)
	encoded := strings.Replace(withTimestamp, reqSnaps, "required-snaps:\n  - foo_bar\n  - bar\n", 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(a, IsNil)
	c.Assert(err, ErrorMatches, `assertion model: invalid snap name in "required-snaps" header: foo_bar`)

	encoded = strings.Replace(withTimestamp, reqSnaps, "required-snaps:\n  - foo\n  - bar-;;''\n", 1)
	a, err = asserts.Decode([]byte(encoded))
	c.Assert(a, IsNil)
	c.Assert(err, ErrorMatches, `assertion model: invalid snap name in "required-snaps" header: bar-;;''`)

	encoded = strings.Replace(withTimestamp, "kernel: baz-linux\n", "kernel: baz-linux_instance\n", 1)
	a, err = asserts.Decode([]byte(encoded))
	c.Assert(a, IsNil)
	c.Assert(err, ErrorMatches, `assertion model: invalid snap name in "kernel" header: baz-linux_instance`)

	encoded = strings.Replace(withTimestamp, "gadget: brand-gadget\n", "gadget: brand-gadget_instance\n", 1)
	a, err = asserts.Decode([]byte(encoded))
	c.Assert(a, IsNil)
	c.Assert(err, ErrorMatches, `assertion model: invalid snap name in "gadget" header: brand-gadget_instance`)

	encoded = strings.Replace(withTimestamp, "base: core18\n", "base: core18_instance\n", 1)
	a, err = asserts.Decode([]byte(encoded))
	c.Assert(a, IsNil)
	c.Assert(err, ErrorMatches, `assertion model: invalid snap name in "base" header: core18_instance`)
}

func (mods modelSuite) TestDecodeValidSnapNames(c *C) {
	// reuse test cases for snap.ValidateName()

	withTimestamp := strings.Replace(modelExample, "TSLINE", mods.tsLine, 1)

	validNames := []string{
		"aa", "aaa", "aaaa",
		"a-a", "aa-a", "a-aa", "a-b-c",
		"a0", "a-0", "a-0a",
		"01game", "1-or-2",
		// a regexp stresser
		"u-94903713687486543234157734673284536758",
	}
	for _, name := range validNames {
		encoded := strings.Replace(withTimestamp, "kernel: baz-linux\n", fmt.Sprintf("kernel: %s\n", name), 1)
		a, err := asserts.Decode([]byte(encoded))
		c.Assert(err, IsNil)
		model := a.(*asserts.Model)
		c.Check(model.Kernel(), Equals, name)
	}
	invalidNames := []string{
		// name cannot be empty, never reaches snap name validation
		"",
		// too short (min 2 chars)
		"a",
		// names cannot be too long
		"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
		"xxxxxxxxxxxxxxxxxxxx-xxxxxxxxxxxxxxxxxxxx",
		"1111111111111111111111111111111111111111x",
		"x1111111111111111111111111111111111111111",
		"x-x-x-x-x-x-x-x-x-x-x-x-x-x-x-x-x-x-x-x-x",
		// a regexp stresser
		"u-9490371368748654323415773467328453675-",
		// dashes alone are not a name
		"-", "--",
		// double dashes in a name are not allowed
		"a--a",
		// name should not end with a dash
		"a-",
		// name cannot have any spaces in it
		"a ", " a", "a a",
		// a number alone is not a name
		"0", "123",
		// identifier must be plain ASCII
		"日本語", "한글", "ру́сский язы́к",
		// instance names are invalid too
		"foo_bar", "x_1",
	}
	for _, name := range invalidNames {
		encoded := strings.Replace(withTimestamp, "kernel: baz-linux\n", fmt.Sprintf("kernel: %s\n", name), 1)
		a, err := asserts.Decode([]byte(encoded))
		c.Assert(a, IsNil)
		if name != "" {
			c.Assert(err, ErrorMatches, `assertion model: invalid snap name in "kernel" header: .*`)
		} else {
			c.Assert(err, ErrorMatches, `assertion model: "kernel" header should not be empty`)
		}
	}
}

func (mods *modelSuite) TestDecodeSerialAuthorityIsOptional(c *C) {
	withTimestamp := strings.Replace(modelExample, "TSLINE", mods.tsLine, 1)
	encoded := strings.Replace(withTimestamp, serialAuths, "", 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	model := a.(*asserts.Model)
	// the default is just to accept the brand itself
	c.Check(model.SerialAuthority(), DeepEquals, []string{"brand-id1"})

	encoded = strings.Replace(withTimestamp, serialAuths, "serial-authority:\n  - foo\n  - bar\n", 1)
	a, err = asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	model = a.(*asserts.Model)
	// the brand is always added implicitly
	c.Check(model.SerialAuthority(), DeepEquals, []string{"brand-id1", "foo", "bar"})
}

func (mods *modelSuite) TestDecodeSystemUserAuthorityIsOptional(c *C) {
	withTimestamp := strings.Replace(modelExample, "TSLINE", mods.tsLine, 1)
	encoded := strings.Replace(withTimestamp, sysUserAuths, "", 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	model := a.(*asserts.Model)
	// the default is just to accept the brand itself
	c.Check(model.SystemUserAuthority(), DeepEquals, []string{"brand-id1"})

	encoded = strings.Replace(withTimestamp, sysUserAuths, "system-user-authority:\n  - foo\n  - bar\n", 1)
	a, err = asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	model = a.(*asserts.Model)
	// the brand is always added implicitly, it can always sign
	// a new revision of the model anyway
	c.Check(model.SystemUserAuthority(), DeepEquals, []string{"brand-id1", "foo", "bar"})
}

func (mods *modelSuite) TestDecodePreseedAuthorityIsOptional(c *C) {
	withTimestamp := strings.Replace(modelExample, "TSLINE", mods.tsLine, 1)
	encoded := strings.Replace(withTimestamp, preseedAuths, "", 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	model := a.(*asserts.Model)
	// the default is just to accept the brand itself
	c.Check(model.PreseedAuthority(), DeepEquals, []string{"brand-id1"})

	encoded = strings.Replace(withTimestamp, preseedAuths, "preseed-authority:\n  - foo\n  - bar\n", 1)
	a, err = asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	model = a.(*asserts.Model)
	// the brand is always added implicitly
	c.Check(model.PreseedAuthority(), DeepEquals, []string{"brand-id1", "foo", "bar"})
}

func (mods *modelSuite) TestDecodeKernelTrack(c *C) {
	withTimestamp := strings.Replace(modelExample, "TSLINE", mods.tsLine, 1)
	encoded := strings.Replace(withTimestamp, "kernel: baz-linux\n", "kernel: baz-linux=18\n", 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	model := a.(*asserts.Model)
	c.Check(model.KernelSnap(), DeepEquals, &asserts.ModelSnap{
		Name:        "baz-linux",
		SnapType:    "kernel",
		Modes:       []string{"run"},
		PinnedTrack: "18",
		Presence:    "required",
	})
	c.Check(model.Kernel(), Equals, "baz-linux")
	c.Check(model.KernelTrack(), Equals, "18")
}

func (mods *modelSuite) TestDecodeGadgetTrack(c *C) {
	withTimestamp := strings.Replace(modelExample, "TSLINE", mods.tsLine, 1)
	encoded := strings.Replace(withTimestamp, "gadget: brand-gadget\n", "gadget: brand-gadget=18\n", 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	model := a.(*asserts.Model)
	c.Check(model.GadgetSnap(), DeepEquals, &asserts.ModelSnap{
		Name:        "brand-gadget",
		SnapType:    "gadget",
		Modes:       []string{"run"},
		PinnedTrack: "18",
		Presence:    "required",
	})
	c.Check(model.Gadget(), Equals, "brand-gadget")
	c.Check(model.GadgetTrack(), Equals, "18")
}

const (
	modelErrPrefix = "assertion model: "
)

func (mods *modelSuite) TestDecodeInvalid(c *C) {
	encoded := strings.Replace(modelExample, "TSLINE", mods.tsLine, 1)

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"series: 16\n", "", `"series" header is mandatory`},
		{"series: 16\n", "series: \n", `"series" header should not be empty`},
		{"brand-id: brand-id1\n", "", `"brand-id" header is mandatory`},
		{"brand-id: brand-id1\n", "brand-id: \n", `"brand-id" header should not be empty`},
		{"brand-id: brand-id1\n", "brand-id: random\n", `authority-id and brand-id must match, model assertions are expected to be signed by the brand: "brand-id1" != "random"`},
		{"model: baz-3000\n", "", `"model" header is mandatory`},
		{"model: baz-3000\n", "model: \n", `"model" header should not be empty`},
		{"model: baz-3000\n", "model: baz/3000\n", `"model" primary key header cannot contain '/'`},
		// lift this restriction at a later point
		{"model: baz-3000\n", "model: BAZ-3000\n", `"model" header cannot contain uppercase letters`},
		{"display-name: Baz 3000\n", "display-name:\n  - xyz\n", `"display-name" header must be a string`},
		{"architecture: amd64\n", "", `"architecture" header is mandatory`},
		{"architecture: amd64\n", "architecture: \n", `"architecture" header should not be empty`},
		{"gadget: brand-gadget\n", "", `"gadget" header is mandatory`},
		{"gadget: brand-gadget\n", "gadget: \n", `"gadget" header should not be empty`},
		{"gadget: brand-gadget\n", "gadget: brand-gadget=x/x/x\n", `"gadget" channel selector must be a track name only`},
		{"gadget: brand-gadget\n", "gadget: brand-gadget=stable\n", `"gadget" channel selector must be a track name`},
		{"gadget: brand-gadget\n", "gadget: brand-gadget=18/beta\n", `"gadget" channel selector must be a track name only`},
		{"gadget: brand-gadget\n", "gadget:\n  - xyz \n", `"gadget" header must be a string`},
		{"kernel: baz-linux\n", "", `"kernel" header is mandatory`},
		{"kernel: baz-linux\n", "kernel: \n", `"kernel" header should not be empty`},
		{"kernel: baz-linux\n", "kernel: baz-linux=x/x/x\n", `"kernel" channel selector must be a track name only`},
		{"kernel: baz-linux\n", "kernel: baz-linux=stable\n", `"kernel" channel selector must be a track name`},
		{"kernel: baz-linux\n", "kernel: baz-linux=18/beta\n", `"kernel" channel selector must be a track name only`},
		{"kernel: baz-linux\n", "kernel:\n  - xyz \n", `"kernel" header must be a string`},
		{"base: core18\n", "base:\n  - xyz \n", `"base" header must be a string`},
		{"store: brand-store\n", "store:\n  - xyz\n", `"store" header must be a string`},
		{mods.tsLine, "", `"timestamp" header is mandatory`},
		{mods.tsLine, "timestamp: \n", `"timestamp" header should not be empty`},
		{mods.tsLine, "timestamp: 12:30\n", `"timestamp" header is not a RFC3339 date: .*`},
		{reqSnaps, "required-snaps: foo\n", `"required-snaps" header must be a list of strings`},
		{reqSnaps, "required-snaps:\n  -\n    - nested\n", `"required-snaps" header must be a list of strings`},
		{serialAuths, "serial-authority:\n  a: 1\n", `"serial-authority" header must be a list of account ids`},
		{serialAuths, "serial-authority:\n  - 5_6\n", `"serial-authority" header must be a list of account ids`},
		{serialAuths, "serial-authority: *\n", `"serial-authority" header must be a list of account ids`},
		{sysUserAuths, "system-user-authority:\n  a: 1\n", `"system-user-authority" header must be '\*' or a list of account ids`},
		{sysUserAuths, "system-user-authority:\n  - 5_6\n", `"system-user-authority" header must be '\*' or a list of account ids`},
		{preseedAuths, "preseed-authority:\n  a: 1\n", `"preseed-authority" header must be a list of account ids`},
		{preseedAuths, "preseed-authority:\n  - 5_6\n", `"preseed-authority" header must be a list of account ids`},
		{preseedAuths, "preseed-authority: *\n", `"preseed-authority" header must be a list of account ids`},
		{reqSnaps, "grade: dangerous\n", `cannot specify a grade for model without the extended snaps header`},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, modelErrPrefix+test.expectedErr)
	}
}

func (mods *modelSuite) TestModelCheck(c *C) {
	ex, err := asserts.Decode([]byte(strings.Replace(modelExample, "TSLINE", mods.tsLine, 1)))
	c.Assert(err, IsNil)

	storeDB, db := makeStoreAndCheckDB(c)
	brandDB := setup3rdPartySigning(c, "brand-id1", storeDB, db)

	headers := ex.Headers()
	headers["brand-id"] = brandDB.AuthorityID
	headers["timestamp"] = time.Now().Format(time.RFC3339)
	model, err := brandDB.Sign(asserts.ModelType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(model)
	c.Assert(err, IsNil)
}

func (mods *modelSuite) TestModelCheckInconsistentTimestamp(c *C) {
	ex, err := asserts.Decode([]byte(strings.Replace(modelExample, "TSLINE", mods.tsLine, 1)))
	c.Assert(err, IsNil)

	storeDB, db := makeStoreAndCheckDB(c)
	brandDB := setup3rdPartySigning(c, "brand-id1", storeDB, db)

	headers := ex.Headers()
	headers["brand-id"] = brandDB.AuthorityID
	headers["timestamp"] = "2011-01-01T14:00:00Z"
	model, err := brandDB.Sign(asserts.ModelType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(model)
	c.Assert(err, ErrorMatches, `model assertion timestamp "2011-01-01 14:00:00 \+0000 UTC" outside of signing key validity \(key valid since.*\)`)
}

func (mods *modelSuite) TestClassicDecodeOK(c *C) {
	encoded := strings.Replace(classicModelExample, "TSLINE", mods.tsLine, 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.ModelType)
	model := a.(*asserts.Model)
	c.Check(model.AuthorityID(), Equals, "brand-id1")
	c.Check(model.Timestamp(), Equals, mods.ts)
	c.Check(model.Series(), Equals, "16")
	c.Check(model.BrandID(), Equals, "brand-id1")
	c.Check(model.Model(), Equals, "baz-3000")
	c.Check(model.DisplayName(), Equals, "Baz 3000")
	c.Check(model.Classic(), Equals, true)
	c.Check(model.Distribution(), Equals, "")
	c.Check(model.Architecture(), Equals, "amd64")
	c.Check(model.GadgetSnap(), DeepEquals, &asserts.ModelSnap{
		Name:     "brand-gadget",
		SnapType: "gadget",
		Modes:    []string{"run"},
		Presence: "required",
	})
	c.Check(model.Gadget(), Equals, "brand-gadget")
	c.Check(model.KernelSnap(), IsNil)
	c.Check(model.Kernel(), Equals, "")
	c.Check(model.KernelTrack(), Equals, "")
	c.Check(model.Base(), Equals, "")
	c.Check(model.BaseSnap(), IsNil)
	c.Check(model.Store(), Equals, "brand-store")
	essentialSnaps := model.EssentialSnaps()
	c.Check(essentialSnaps, DeepEquals, []*asserts.ModelSnap{
		model.GadgetSnap(),
	})
	snaps := model.SnapsWithoutEssential()
	c.Check(snaps, DeepEquals, []*asserts.ModelSnap{
		{
			Name:     "foo",
			Modes:    []string{"run"},
			Presence: "required",
		},
		{
			Name:     "bar",
			Modes:    []string{"run"},
			Presence: "required",
		},
	})
	// gadget included
	reqSnaps := naming.NewSnapSet(model.RequiredWithEssentialSnaps())
	for _, e := range essentialSnaps {
		c.Check(reqSnaps.Contains(e), Equals, true)
	}
	for _, s := range snaps {
		c.Check(reqSnaps.Contains(s), Equals, true)
	}
	c.Check(reqSnaps.Size(), Equals, len(essentialSnaps)+len(snaps))
	// gadget excluded
	noEssential := naming.NewSnapSet(model.RequiredNoEssentialSnaps())
	for _, e := range essentialSnaps {
		c.Check(noEssential.Contains(e), Equals, false)
	}
	for _, s := range snaps {
		c.Check(noEssential.Contains(s), Equals, true)
	}
	c.Check(noEssential.Size(), Equals, len(snaps))
}

func (mods *modelSuite) TestClassicDecodeInvalid(c *C) {
	encoded := strings.Replace(classicModelExample, "TSLINE", mods.tsLine, 1)

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"classic: true\n", "classic: foo\n", `"classic" header must be 'true' or 'false'`},
		{"architecture: amd64\n", "architecture:\n  - foo\n", `"architecture" header must be a string`},
		{"gadget: brand-gadget\n", "gadget:\n  - foo\n", `"gadget" header must be a string`},
		{"gadget: brand-gadget\n", "kernel: brand-kernel\n", `cannot specify a kernel with a non-extended classic model`},
		{"gadget: brand-gadget\n", "base: some-base\n", `cannot specify a base with a non-extended classic model`},
		{"gadget: brand-gadget\n", "gadget:\n  - xyz\n", `"gadget" header must be a string`},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, modelErrPrefix+test.expectedErr)
	}
}

func (mods *modelSuite) TestClassicDecodeGadgetAndArchOptional(c *C) {
	encoded := strings.Replace(classicModelExample, "TSLINE", mods.tsLine, 1)
	encoded = strings.Replace(encoded, "gadget: brand-gadget\n", "", 1)
	encoded = strings.Replace(encoded, "architecture: amd64\n", "", 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.ModelType)
	model := a.(*asserts.Model)
	c.Check(model.Classic(), Equals, true)
	c.Check(model.Distribution(), Equals, "")
	c.Check(model.Architecture(), Equals, "")
	c.Check(model.GadgetSnap(), IsNil)
	c.Check(model.Gadget(), Equals, "")
	c.Check(model.GadgetTrack(), Equals, "")
}

func (mods *modelSuite) TestWithSnapsDecodeOK(c *C) {
	tt := []struct {
		modelRaw  string
		isClassic bool
	}{
		{modelRaw: core20ModelExample, isClassic: false},
		{modelRaw: classicModelWithSnapsExample, isClassic: true},
	}

	for _, t := range tt {
		mods.testWithSnapsDecodeOK(c, t.modelRaw, t.isClassic)
	}
}

func (mods *modelSuite) testWithSnapsDecodeOK(c *C, modelRaw string, isClassic bool) {
	encoded := strings.Replace(modelRaw, "TSLINE", mods.tsLine, 1)
	encoded = strings.Replace(encoded, "OTHER", "", 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.ModelType)
	model := a.(*asserts.Model)
	c.Check(model.AuthorityID(), Equals, "brand-id1")
	c.Check(model.Timestamp(), Equals, mods.ts)
	c.Check(model.Series(), Equals, "16")
	c.Check(model.BrandID(), Equals, "brand-id1")
	c.Check(model.Model(), Equals, "baz-3000")
	c.Check(model.DisplayName(), Equals, "Baz 3000")
	c.Check(model.Architecture(), Equals, "amd64")
	c.Check(model.Classic(), Equals, isClassic)
	if isClassic {
		c.Check(model.Distribution(), Equals, "ubuntu")
	} else {
		c.Check(model.Distribution(), Equals, "")
	}
	c.Check(model.GadgetSnap(), DeepEquals, &asserts.ModelSnap{
		Name:           "brand-gadget",
		SnapID:         "brandgadgetdidididididididididid",
		SnapType:       "gadget",
		Modes:          []string{"run", "ephemeral"},
		DefaultChannel: "latest/stable",
		Presence:       "required",
	})
	c.Check(model.Gadget(), Equals, "brand-gadget")
	c.Check(model.GadgetTrack(), Equals, "")
	c.Check(model.KernelSnap(), DeepEquals, &asserts.ModelSnap{
		Name:           "baz-linux",
		SnapID:         "bazlinuxidididididididididididid",
		SnapType:       "kernel",
		Modes:          []string{"run", "ephemeral"},
		DefaultChannel: "20",
		Presence:       "required",
	})
	c.Check(model.Kernel(), Equals, "baz-linux")
	c.Check(model.KernelTrack(), Equals, "")
	c.Check(model.Base(), Equals, "core20")
	c.Check(model.BaseSnap(), DeepEquals, &asserts.ModelSnap{
		Name:           "core20",
		SnapID:         naming.WellKnownSnapID("core20"),
		SnapType:       "base",
		Modes:          []string{"run", "ephemeral"},
		DefaultChannel: "latest/stable",
		Presence:       "required",
	})
	c.Check(model.Store(), Equals, "brand-store")
	c.Check(model.Grade(), Equals, asserts.ModelSecured)
	c.Check(model.StorageSafety(), Equals, asserts.StorageSafetyEncrypted)
	essentialSnaps := model.EssentialSnaps()
	c.Check(essentialSnaps, DeepEquals, []*asserts.ModelSnap{
		model.KernelSnap(),
		model.BaseSnap(),
		model.GadgetSnap(),
	})
	snaps := model.SnapsWithoutEssential()
	c.Check(snaps, DeepEquals, []*asserts.ModelSnap{
		{
			Name:           "other-base",
			SnapID:         "otherbasedididididididididididid",
			SnapType:       "base",
			Modes:          []string{"run"},
			DefaultChannel: "latest/stable",
			Presence:       "required",
		},
		{
			Name:           "nm",
			SnapID:         "nmididididididididididididididid",
			SnapType:       "app",
			Modes:          []string{"ephemeral", "run"},
			DefaultChannel: "1.0",
			Presence:       "required",
		},
		{
			Name:           "myapp",
			SnapID:         "myappdididididididididididididid",
			SnapType:       "app",
			Modes:          []string{"run"},
			DefaultChannel: "2.0",
			Presence:       "required",
		},
		{
			Name:           "myappopt",
			SnapID:         "myappoptidididididididididididid",
			SnapType:       "app",
			Modes:          []string{"run"},
			DefaultChannel: "latest/stable",
			Presence:       "optional",
			Classic:        isClassic,
		},
	})
	// essential snaps included
	reqSnaps := naming.NewSnapSet(model.RequiredWithEssentialSnaps())
	for _, e := range essentialSnaps {
		c.Check(reqSnaps.Contains(e), Equals, true)
	}
	for _, s := range snaps {
		c.Check(reqSnaps.Contains(s), Equals, s.Presence == "required")
	}
	c.Check(reqSnaps.Size(), Equals, len(essentialSnaps)+len(snaps)-1)
	// essential snaps excluded
	noEssential := naming.NewSnapSet(model.RequiredNoEssentialSnaps())
	for _, e := range essentialSnaps {
		c.Check(noEssential.Contains(e), Equals, false)
	}
	for _, s := range snaps {
		c.Check(noEssential.Contains(s), Equals, s.Presence == "required")
	}
	c.Check(noEssential.Size(), Equals, len(snaps)-1)

	c.Check(model.SystemUserAuthority(), HasLen, 0)
	c.Check(model.SerialAuthority(), DeepEquals, []string{"brand-id1"})
	c.Check(model.PreseedAuthority(), DeepEquals, []string{"brand-id1"})
}

func (mods *modelSuite) TestCore20ExplictBootBase(c *C) {
	encoded := strings.Replace(core20ModelExample, "TSLINE", mods.tsLine, 1)
	encoded = strings.Replace(encoded, "OTHER", `  -
    name: core20
    id: core20ididididididididididididid
    type: base
    default-channel: latest/candidate
`, 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.ModelType)
	model := a.(*asserts.Model)
	c.Check(model.BaseSnap(), DeepEquals, &asserts.ModelSnap{
		Name:           "core20",
		SnapID:         "core20ididididididididididididid",
		SnapType:       "base",
		Modes:          []string{"run", "ephemeral"},
		DefaultChannel: "latest/candidate",
		Presence:       "required",
	})
}

func (mods *modelSuite) TestCore20ExplictSnapd(c *C) {
	encoded := strings.Replace(core20ModelExample, "TSLINE", mods.tsLine, 1)
	encoded = strings.Replace(encoded, "OTHER", `  -
    name: snapd
    id: snapdidididididididididididididd
    type: snapd
    default-channel: latest/edge
`, 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.ModelType)
	model := a.(*asserts.Model)
	snapdSnap := model.EssentialSnaps()[0]
	c.Check(snapdSnap, DeepEquals, &asserts.ModelSnap{
		Name:           "snapd",
		SnapID:         "snapdidididididididididididididd",
		SnapType:       "snapd",
		Modes:          []string{"run", "ephemeral"},
		DefaultChannel: "latest/edge",
		Presence:       "required",
	})
}

func (mods *modelSuite) TestCore20GradeOptionalDefaultSigned(c *C) {
	encoded := strings.Replace(core20ModelExample, "TSLINE", mods.tsLine, 1)
	encoded = strings.Replace(encoded, "OTHER", "", 1)
	encoded = strings.Replace(encoded, "grade: secured\n", "", 1)

	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.ModelType)
	model := a.(*asserts.Model)
	c.Check(model.Grade(), Equals, asserts.ModelSigned)
}

func (mods *modelSuite) TestCore20ValidGrades(c *C) {
	encoded := strings.Replace(core20ModelExample, "TSLINE", mods.tsLine, 1)
	encoded = strings.Replace(encoded, "OTHER", "", 1)

	for _, grade := range []string{"signed", "secured", "dangerous"} {
		ex := strings.Replace(encoded, "grade: secured\n", fmt.Sprintf("grade: %s\n", grade), 1)
		a, err := asserts.Decode([]byte(ex))
		c.Assert(err, IsNil)
		c.Check(a.Type(), Equals, asserts.ModelType)
		model := a.(*asserts.Model)
		c.Check(string(model.Grade()), Equals, grade)
	}
}

func (mods *modelSuite) TestModelGradeCode(c *C) {
	for i, grade := range []asserts.ModelGrade{asserts.ModelGradeUnset, asserts.ModelDangerous, asserts.ModelSigned, asserts.ModelSecured} {
		// unset is represented as zero
		code := 0
		if i > 0 {
			// have some space between grades to add new ones
			n := (i - 1) * 8
			if n == 0 {
				n = 1 // dangerous
			}
			// lower 16 bits are reserved
			code = n << 16
		}
		c.Check(grade.Code(), Equals, uint32(code))
	}
}

func (mods *modelSuite) TestCore20GradeDangerous(c *C) {
	encoded := strings.Replace(core20ModelExample, "TSLINE", mods.tsLine, 1)
	encoded = strings.Replace(encoded, "OTHER", "", 1)
	encoded = strings.Replace(encoded, "grade: secured\n", "grade: dangerous\n", 1)
	// snap ids are optional with grade dangerous to allow working
	// with local/not pushed yet to the store snaps
	encoded = strings.Replace(encoded, "    id: myappdididididididididididididid\n", "", 1)
	encoded = strings.Replace(encoded, "    id: brandgadgetdidididididididididid\n", "", 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.ModelType)
	model := a.(*asserts.Model)
	c.Check(model.Grade(), Equals, asserts.ModelDangerous)
	snaps := model.SnapsWithoutEssential()
	c.Check(snaps[len(snaps)-2], DeepEquals, &asserts.ModelSnap{
		Name:           "myapp",
		SnapType:       "app",
		Modes:          []string{"run"},
		DefaultChannel: "2.0",
		Presence:       "required",
	})
}

func (mods *modelSuite) TestCore20ValidStorageSafety(c *C) {
	encoded := strings.Replace(core20ModelExample, "TSLINE", mods.tsLine, 1)
	encoded = strings.Replace(encoded, "OTHER", "", 1)
	encoded = strings.Replace(encoded, "grade: secured\n", "grade: signed\n", 1)

	for _, tc := range []struct {
		ss  asserts.StorageSafety
		sss string
	}{
		{asserts.StorageSafetyPreferEncrypted, "prefer-encrypted"},
		{asserts.StorageSafetyPreferUnencrypted, "prefer-unencrypted"},
		{asserts.StorageSafetyEncrypted, "encrypted"},
	} {
		ex := strings.Replace(encoded, "storage-safety: encrypted\n", fmt.Sprintf("storage-safety: %s\n", tc.sss), 1)
		a, err := asserts.Decode([]byte(ex))
		c.Assert(err, IsNil)
		c.Check(a.Type(), Equals, asserts.ModelType)
		model := a.(*asserts.Model)
		c.Check(model.StorageSafety(), Equals, tc.ss)
	}
}

func (mods *modelSuite) TestCore20DefaultStorageSafetySecured(c *C) {
	encoded := strings.Replace(core20ModelExample, "TSLINE", mods.tsLine, 1)
	encoded = strings.Replace(encoded, "OTHER", "", 1)
	ex := strings.Replace(encoded, "storage-safety: encrypted\n", "", 1)

	a, err := asserts.Decode([]byte(ex))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.ModelType)
	model := a.(*asserts.Model)
	c.Check(model.StorageSafety(), Equals, asserts.StorageSafetyEncrypted)
}

func (mods *modelSuite) TestCore20DefaultStorageSafetySignedDangerous(c *C) {
	encoded := strings.Replace(core20ModelExample, "TSLINE", mods.tsLine, 1)
	encoded = strings.Replace(encoded, "OTHER", "", 1)
	encoded = strings.Replace(encoded, "storage-safety: encrypted\n", "", 1)

	for _, grade := range []string{"dangerous", "signed"} {
		ex := strings.Replace(encoded, "grade: secured\n", fmt.Sprintf("grade: %s\n", grade), 1)
		a, err := asserts.Decode([]byte(ex))
		c.Assert(err, IsNil)
		c.Check(a.Type(), Equals, asserts.ModelType)
		model := a.(*asserts.Model)
		c.Check(model.StorageSafety(), Equals, asserts.StorageSafetyPreferEncrypted)
	}
}

func (mods *modelSuite) TestWithSnapsDecodeInvalid(c *C) {
	tt := []struct {
		modelRaw  string
		isClassic bool
	}{
		{modelRaw: core20ModelExample, isClassic: false},
		{modelRaw: classicModelWithSnapsExample, isClassic: true},
	}

	for _, t := range tt {
		mods.testWithSnapsDecodeInvalid(c, t.modelRaw, t.isClassic)
	}
}

func (mods *modelSuite) testWithSnapsDecodeInvalid(c *C, modelRaw string, isClassic bool) {
	encoded := strings.Replace(modelRaw, "TSLINE", mods.tsLine, 1)

	snapsStanza := encoded[strings.Index(encoded, "snaps:"):strings.Index(encoded, "grade:")]

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"base: core20\n", "", `"base" header is mandatory`},
		{"base: core20\n", "base: alt-base\n", `cannot specify not well-known base "alt-base" without a corresponding "snaps" header entry`},
		{snapsStanza, "snaps: snap\n", `"snaps" header must be a list of maps`},
		{snapsStanza, "snaps:\n  - snap\n", `"snaps" header must be a list of maps`},
		{"name: myapp\n", "other: 1\n", `"name" of snap is mandatory`},
		{"name: myapp\n", "name: myapp_2\n", `invalid snap name "myapp_2"`},
		{"id: myappdididididididididididididid\n", "id: 2\n", `"id" of snap "myapp" contains invalid characters: "2"`},
		{"    id: myappdididididididididididididid\n", "", `"id" of snap "myapp" is mandatory for secured grade model`},
		{"type: gadget\n", "type:\n      - g\n", `"type" of snap "brand-gadget" must be a string`},
		{"type: app\n", "type: thing\n", `"type" of snap "myappopt" must be one of must be one of app|base|gadget|kernel|core|snapd`},
		{"modes:\n      - run\n", "modes: run\n", `"modes" of snap "other-base" must be a list of strings`},
		{"default-channel: 20\n", "default-channel: edge\n", `default channel for snap "baz-linux" must specify a track`},
		{"default-channel: 2.0\n", "default-channel:\n      - x\n", `"default-channel" of snap "myapp" must be a string`},
		{"default-channel: 2.0\n", "default-channel: 2.0/xyz/z\n", `invalid default channel for snap "myapp": invalid risk in channel name: 2.0/xyz/z`},
		{"presence: optional\n", "presence:\n      - opt\n", `"presence" of snap "myappopt" must be a string`},
		{"presence: optional\n", "presence: no\n", `"presence" of snap "myappopt" must be one of must be one of required|optional`},
		{"OTHER", "  -\n    name: myapp\n    id: myappdididididididididididididid\n", `cannot list the same snap "myapp" multiple times`},
		{"OTHER", "  -\n    name: myapp2\n    id: myappdididididididididididididid\n", `cannot specify the same snap id "myappdididididididididididididid" multiple times, specified for snaps "myapp" and "myapp2"`},
		{"OTHER", "  -\n    name: kernel2\n    id: kernel2didididididididididididid\n    type: kernel\n", `cannot specify multiple kernel snaps: "baz-linux" and "kernel2"`},
		{"OTHER", "  -\n    name: gadget2\n    id: gadget2didididididididididididid\n    type: gadget\n", `cannot specify multiple gadget snaps: "brand-gadget" and "gadget2"`},
		{"type: gadget\n", "type: gadget\n    presence: required\n", `essential snaps are always available, cannot specify presence for snap "brand-gadget"`},
		{"type: gadget\n", "type: gadget\n    modes:\n      - run\n", `essential snaps are always available, cannot specify modes of snap "brand-gadget"`},
		{"type: kernel\n", "type: kernel\n    presence: required\n", `essential snaps are always available, cannot specify presence for snap "baz-linux"`},
		{"OTHER", "  -\n    name: core20\n    id: core20ididididididididididididid\n    type: base\n    presence: optional\n", `essential snaps are always available, cannot specify presence for snap "core20"`},
		{"OTHER", "  -\n    name: core20\n    id: core20ididididididididididididid\n    type: app\n", `boot base "core20" must specify type "base", not "app"`},
		{"OTHER", "kernel: foo\n", `cannot specify separate "kernel" header once using the extended snaps header`},
		{"OTHER", "gadget: foo\n", `cannot specify separate "gadget" header once using the extended snaps header`},
		{"OTHER", "required-snaps:\n  - foo\n", `cannot specify separate "required-snaps" header once using the extended snaps header`},
		{"grade: secured\n", "grade: foo\n", `grade for model must be secured|signed|dangerous`},
		{"storage-safety: encrypted\n", "storage-safety: foo\n", `storage-safety for model must be encrypted\|prefer-encrypted\|prefer-unencrypted, not "foo"`},
		{"storage-safety: encrypted\n", "storage-safety: prefer-unencrypted\n", `secured grade model must not have storage-safety overridden, only "encrypted" is valid`},
	}
	if isClassic {
		classicInvalid := []struct{ original, invalid, expectedErr string }{
			{"distribution: ubuntu\n", "", `"distribution" header is mandatory, see distribution ID in os-release spec`},
			{"distribution: ubuntu\n", "distribution: Ubuntu\n", `"distribution" header contains invalid characters: "Ubuntu", see distribution ID in os-release spec`},
			{"distribution: ubuntu\n", "distribution: *buntu\n", `"distribution" header contains invalid characters: "\*buntu", see distribution ID in os-release spec`},
			{"type: gadget\n", "type: app\n", `cannot specify a kernel in an extended classic model without a model gadget`},
			{"  classic: true\n", "  classic: what", `"classic" of snap "myappopt" must be 'true' or 'false'`},
			{"OTHER", `    modes:
      - ephemeral
      - run
`, `classic snap "myappopt" not allowed outside of run mode: \[ephemeral run\]`},
			{"OTHER", `    modes:
      - install
`, `classic snap "myappopt" not allowed outside of run mode: \[install\]`},
			{`    type: app
    presence: optional
`, `    type: base
    presence: optional
`, `snap "myappopt" cannot be classic with type "base" instead of app`},
			{"\nclassic: true\ndistribution: ubuntu\n", "\nclassic: false\n", `snap "myappopt" cannot be classic in non-classic model`},
		}
		invalidTests = append(invalidTests, classicInvalid...)
	} else {
		coreInvalid := []struct{ original, invalid, expectedErr string }{
			{"OTHER", "distribution: ubuntu\n", `cannot specify distribution for model unless it is classic and has an extended snaps header`},
			{"type: gadget\n", "type: app\n", `one "snaps" header entry must specify the model gadget`},
			{"type: kernel\n", "type: app\n", `one "snaps" header entry must specify the model kernel`},
		}
		invalidTests = append(invalidTests, coreInvalid...)
	}
	for _, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		invalid = strings.Replace(invalid, "OTHER", "", 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, modelErrPrefix+test.expectedErr)
	}
}

func (mods *modelSuite) TestClassicWithSnapsMinimalDecodeOK(c *C) {
	// XXX support also omitting the base?
	encoded := strings.Replace(classicModelWithSnapsExample, "TSLINE", mods.tsLine, 1)
	encoded = strings.Replace(encoded, "OTHER", "", 1)
	tests := []struct {
		originalFrag string
		changedFrag  string
		hasGadget    bool
	}{
		// no kernel and no gadget
		{`
  -
    name: baz-linux
    id: bazlinuxidididididididididididid
    type: kernel
    default-channel: 20
  -
    name: brand-gadget
    id: brandgadgetdidididididididididid
    type: gadget`, "", false},
		// no kernel but a gadget
		{`
  -
    name: baz-linux
    id: bazlinuxidididididididididididid
    type: kernel
    default-channel: 20`, "", true},
	}

	for _, t := range tests {
		minimal := strings.Replace(encoded, t.originalFrag, t.changedFrag, 1)
		a, err := asserts.Decode([]byte(minimal))
		c.Assert(err, IsNil)
		c.Check(a.Type(), Equals, asserts.ModelType)
		model := a.(*asserts.Model)
		c.Check(model.Architecture(), Equals, "amd64")
		c.Check(model.Classic(), Equals, true)
		c.Check(model.Distribution(), Equals, "ubuntu")
		c.Check(model.Base(), Equals, "core20")
		// no kernel
		c.Check(model.KernelSnap(), IsNil)
		c.Check(model.Kernel(), Equals, "")
		c.Check(model.KernelTrack(), Equals, "")
		c.Check(model.BaseSnap(), DeepEquals, &asserts.ModelSnap{
			Name:           "core20",
			SnapID:         naming.WellKnownSnapID("core20"),
			SnapType:       "base",
			Modes:          []string{"run", "ephemeral"},
			DefaultChannel: "latest/stable",
			Presence:       "required",
		})
		expectedEssSnap := []*asserts.ModelSnap{
			model.BaseSnap(),
		}
		if t.hasGadget {
			c.Check(model.GadgetSnap(), DeepEquals, &asserts.ModelSnap{
				Name:           "brand-gadget",
				SnapID:         "brandgadgetdidididididididididid",
				SnapType:       "gadget",
				Modes:          []string{"run", "ephemeral"},
				DefaultChannel: "latest/stable",
				Presence:       "required",
			})
			c.Check(model.Gadget(), Equals, "brand-gadget")
			c.Check(model.GadgetTrack(), Equals, "")
			expectedEssSnap = append(expectedEssSnap, model.GadgetSnap())
		} else {
			c.Check(model.GadgetSnap(), IsNil)
			c.Check(model.Gadget(), Equals, "")
			c.Check(model.GadgetTrack(), Equals, "")
		}
		c.Check(model.Grade(), Equals, asserts.ModelSecured)
		c.Check(model.StorageSafety(), Equals, asserts.StorageSafetyEncrypted)
		essentialSnaps := model.EssentialSnaps()
		c.Check(essentialSnaps, DeepEquals, expectedEssSnap)
		snaps := model.SnapsWithoutEssential()
		c.Check(snaps, DeepEquals, []*asserts.ModelSnap{
			{
				Name:           "other-base",
				SnapID:         "otherbasedididididididididididid",
				SnapType:       "base",
				Modes:          []string{"run"},
				DefaultChannel: "latest/stable",
				Presence:       "required",
			},
			{
				Name:           "nm",
				SnapID:         "nmididididididididididididididid",
				SnapType:       "app",
				Modes:          []string{"ephemeral", "run"},
				DefaultChannel: "1.0",
				Presence:       "required",
			},
			{
				Name:           "myapp",
				SnapID:         "myappdididididididididididididid",
				SnapType:       "app",
				Modes:          []string{"run"},
				DefaultChannel: "2.0",
				Presence:       "required",
			},
			{
				Name:           "myappopt",
				SnapID:         "myappoptidididididididididididid",
				SnapType:       "app",
				Modes:          []string{"run"},
				DefaultChannel: "latest/stable",
				Presence:       "optional",
				Classic:        true,
			},
		})
		// essential snaps included
		reqSnaps := naming.NewSnapSet(model.RequiredWithEssentialSnaps())
		for _, e := range essentialSnaps {
			c.Check(reqSnaps.Contains(e), Equals, true)
		}
		for _, s := range snaps {
			c.Check(reqSnaps.Contains(s), Equals, s.Presence == "required")
		}
		c.Check(reqSnaps.Size(), Equals, len(essentialSnaps)+len(snaps)-1)
		// essential snaps excluded
		noEssential := naming.NewSnapSet(model.RequiredNoEssentialSnaps())
		for _, e := range essentialSnaps {
			c.Check(noEssential.Contains(e), Equals, false)
		}
		for _, s := range snaps {
			c.Check(noEssential.Contains(s), Equals, s.Presence == "required")
		}
		c.Check(noEssential.Size(), Equals, len(snaps)-1)
	}
}

func (mods *modelSuite) TestModelValidationSetAtSequence(c *C) {
	mvs := &asserts.ModelValidationSet{
		AccountID: "test",
		Name:      "set",
		Mode:      asserts.ModelValidationSetModeEnforced,
	}
	c.Check(mvs.AtSequence(), DeepEquals, &asserts.AtSequence{
		Type:        asserts.ValidationSetType,
		SequenceKey: []string{release.Series, "test", "set"},
		Sequence:    0,
		Pinned:      false,
		Revision:    asserts.RevisionNotKnown,
	})
}

func (mods *modelSuite) TestModelValidationSetAtSequenceNoSequence(c *C) {
	mvs := &asserts.ModelValidationSet{
		AccountID: "test",
		Name:      "set",
		Sequence:  1,
		Mode:      asserts.ModelValidationSetModeEnforced,
	}
	c.Check(mvs.AtSequence(), DeepEquals, &asserts.AtSequence{
		Type:        asserts.ValidationSetType,
		SequenceKey: []string{release.Series, "test", "set"},
		Sequence:    1,
		Pinned:      true,
		Revision:    asserts.RevisionNotKnown,
	})
}

func (mods *modelSuite) TestValidationSetsDecodeInvalid(c *C) {
	encoded := strings.Replace(core20ModelExample, "TSLINE", mods.tsLine, 1)
	tests := []struct {
		frag        string
		expectedErr string
	}{
		// invalid format 1
		{`validation-sets: 12395
`, "assertion model: \"validation-sets\" must be a list of validation sets"},
		// invalid format 2
		{`validation-sets:
  - test
`, "assertion model: entry in \"validation-sets\" is not a valid validation-set"},
		// missing name
		{`validation-sets:
  -
    mode: prefer-enforce
`, "assertion model: \"name\" of validation-set is mandatory"},
		// account-id not a valid string
		{`validation-sets:
  -
    account-id:
      - 1
    name: my-set
    mode: enforce
`, "assertion model: \"account-id\" of validation-set \"my-set\" must be a string"},
		// missing mode
		{`validation-sets:
  -
    name: my-set
`, "assertion model: \"mode\" of validation-set \"brand-id1/my-set\" is mandatory"},
		// invalid value in mode
		{`validation-sets:
  -
    account-id: developer1
    name: my-set
    sequence: 10
    mode: hello
`, "assertion model: \"mode\" of validation-set \"brand-id1/my-set\" must be prefer-enforce|enforce, not \"hello\""},
		// sequence number invalid (not an integer)
		{`validation-sets:
  -
    account-id: developer1
    sequence: foo
    name: my-set
    mode: enforce
`, "assertion model: \"sequence\" of validation-set \"developer1/my-set\" is not an integer: foo"},
		// sequence number invalid (below)
		{`validation-sets:
  -
    account-id: developer1
    sequence: -1
    name: my-set
    mode: enforce
`, "assertion model: \"sequence\" of validation-set \"developer1/my-set\" must be larger than 0 or left unspecified \\(meaning tracking latest\\)"},
		// sequence number invalid (0 is not allowed)
		{`validation-sets:
  -
    account-id: developer1
    sequence: 0
    name: my-set
    mode: enforce
`, "assertion model: \"sequence\" of validation-set \"developer1/my-set\" must be larger than 0 or left unspecified \\(meaning tracking latest\\)"},
		// duplicate validation-set
		{`validation-sets:
  -
    account-id: developer1
    name: my-set
    mode: prefer-enforce
  -
    account-id: developer1
    name: my-set
    mode: enforce
`, "assertion model: cannot add validation set \"developer1/my-set\" twice"},
	}

	for _, t := range tests {
		data := strings.Replace(encoded, "OTHER", t.frag, 1)
		_, err := asserts.Decode([]byte(data))
		c.Check(err, ErrorMatches, t.expectedErr)
	}
}

func (mods *modelSuite) TestValidationSetsDecodeOK(c *C) {
	encoded := strings.Replace(core20ModelExample, "TSLINE", mods.tsLine, 1)
	tests := []struct {
		frag     string
		expected []*asserts.ModelValidationSet
	}{
		// brand validation-set, this should instead use the brand specified
		// by the core20ModelExample, as account-id is not set
		{`validation-sets:
  -
    name: my-set
    mode: prefer-enforce
`,
			[]*asserts.ModelValidationSet{
				{
					AccountID: "brand-id1",
					Name:      "my-set",
					Mode:      asserts.ModelValidationSetModePreferEnforced,
				},
			}},
		// pinned set
		{`validation-sets:
  -
    account-id: developer1
    name: my-set
    sequence: 10
    mode: enforce
`,
			[]*asserts.ModelValidationSet{
				{
					AccountID: "developer1",
					Name:      "my-set",
					Sequence:  10,
					Mode:      asserts.ModelValidationSetModeEnforced,
				},
			}},
		// unpinned set
		{`validation-sets:
  -
    account-id: developer1
    name: my-set
    mode: prefer-enforce
`,
			[]*asserts.ModelValidationSet{
				{
					AccountID: "developer1",
					Name:      "my-set",
					Mode:      asserts.ModelValidationSetModePreferEnforced,
				},
			}},
	}

	for _, t := range tests {
		data := strings.Replace(encoded, "OTHER", t.frag, 1)
		a, err := asserts.Decode([]byte(data))
		c.Assert(err, IsNil)
		c.Check(a.Type(), Equals, asserts.ModelType)
		model := a.(*asserts.Model)
		c.Check(model.Architecture(), Equals, "amd64")
		c.Check(model.Classic(), Equals, false)
		c.Check(model.Base(), Equals, "core20")
		c.Check(model.ValidationSets(), DeepEquals, t.expected)
	}
}

func (mods *modelSuite) TestModelValidationSetSequenceKey(c *C) {
	mvs := &asserts.ModelValidationSet{
		AccountID: "test",
		Name:      "set",
		Sequence:  1,
		Mode:      asserts.ModelValidationSetModeEnforced,
	}

	c.Check(mvs.SequenceKey(), Equals, "16/test/set")
}

func (mods *modelSuite) TestAllSnaps(c *C) {
	encoded := strings.Replace(core20ModelExample, "TSLINE", mods.tsLine, 1)
	encoded = strings.Replace(encoded, "OTHER", "", 1)

	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.ModelType)

	model := a.(*asserts.Model)

	allSnaps := append([]*asserts.ModelSnap(nil), model.EssentialSnaps()...)

	// make sure that we have essential snaps to compare to
	c.Assert(len(allSnaps), testutil.IntGreaterThan, 0)

	essentialLen := len(allSnaps)

	allSnaps = append(allSnaps, model.SnapsWithoutEssential()...)

	// same here, make sure that we have non-essential snaps to compare to
	c.Assert(len(allSnaps), testutil.IntGreaterThan, essentialLen)

	c.Check(model.AllSnaps(), DeepEquals, allSnaps)
}

func (mods *modelSuite) TestDecodeWithComponentsOK(c *C) {
	encoded := strings.Replace(coreModelWithComponentsExample, "TSLINE", mods.tsLine, 1)
	encoded = strings.Replace(encoded, "OTHER", "", 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)

	c.Check(a.Type(), Equals, asserts.ModelType)
	model := a.(*asserts.Model)
	c.Check(model.AuthorityID(), Equals, "brand-id1")
	c.Check(model.Timestamp(), Equals, mods.ts)
	c.Check(model.Series(), Equals, "16")
	c.Check(model.BrandID(), Equals, "brand-id1")
	c.Check(model.Model(), Equals, "baz-3000")
	c.Check(model.DisplayName(), Equals, "Baz 3000")
	c.Check(model.Architecture(), Equals, "amd64")
	c.Check(model.GadgetSnap(), DeepEquals, &asserts.ModelSnap{
		Name:           "brand-gadget",
		SnapID:         "brandgadgetdidididididididididid",
		SnapType:       "gadget",
		Modes:          []string{"run", "ephemeral"},
		Presence:       "required",
		DefaultChannel: "latest/stable",
	})
	c.Check(model.Gadget(), Equals, "brand-gadget")
	c.Check(model.GadgetTrack(), Equals, "")
	c.Check(model.KernelSnap(), DeepEquals, &asserts.ModelSnap{
		Name:           "baz-linux",
		SnapID:         "bazlinuxidididididididididididid",
		SnapType:       "kernel",
		Modes:          []string{"run", "ephemeral"},
		Presence:       "required",
		DefaultChannel: "20",
	})
	c.Check(model.Kernel(), Equals, "baz-linux")
	c.Check(model.KernelTrack(), Equals, "")
	c.Check(model.Base(), Equals, "core24")
	c.Check(model.BaseSnap(), DeepEquals, &asserts.ModelSnap{
		Name:           "core24",
		SnapID:         "dwTAh7MZZ01zyriOZErqd1JynQLiOGvM",
		SnapType:       "base",
		Modes:          []string{"run", "ephemeral"},
		Presence:       "required",
		DefaultChannel: "latest/stable",
	})
	c.Check(model.Store(), Equals, "brand-store")
	c.Check(model.Grade(), Equals, asserts.ModelSecured)
	c.Check(model.StorageSafety(), Equals, asserts.StorageSafetyEncrypted)
	essentialSnaps := model.EssentialSnaps()
	c.Check(essentialSnaps, DeepEquals, []*asserts.ModelSnap{
		model.KernelSnap(),
		model.BaseSnap(),
		model.GadgetSnap(),
	})
	snaps := model.SnapsWithoutEssential()
	c.Check(snaps, DeepEquals, []*asserts.ModelSnap{
		{
			Name:           "other-base",
			SnapID:         "otherbasedididididididididididid",
			SnapType:       "base",
			Modes:          []string{"run"},
			DefaultChannel: "latest/stable",
			Presence:       "required",
		},
		{
			Name:           "nm",
			SnapID:         "nmididididididididididididididid",
			SnapType:       "app",
			Modes:          []string{"ephemeral", "run"},
			DefaultChannel: "1.0",
			Presence:       "required",
			Components: map[string]asserts.ModelComponent{
				"comp1": {
					Presence: "optional",
					Modes:    []string{"ephemeral"},
				},
				"comp2": {
					Presence: "required",
					Modes:    []string{"ephemeral", "run"},
				},
			},
		},
		{
			Name:           "myapp",
			SnapID:         "myappdididididididididididididid",
			SnapType:       "app",
			Modes:          []string{"ephemeral", "run"},
			DefaultChannel: "2.0",
			Presence:       "optional",
			Components: map[string]asserts.ModelComponent{
				"comp1": {
					Presence: "optional",
					Modes:    []string{"ephemeral", "run"},
				},
				"comp2": {
					Presence: "required",
					Modes:    []string{"ephemeral", "run"},
				},
			},
		},
		{
			Name:           "myappopt",
			SnapID:         "myappoptidididididididididididid",
			SnapType:       "app",
			DefaultChannel: "latest/stable",
			Presence:       "required",
			Modes:          []string{"run"},
			Components: map[string]asserts.ModelComponent{
				"comp1": {
					Presence: "optional",
					Modes:    []string{"run"},
				},
				"comp2": {
					Presence: "required",
					Modes:    []string{"run"},
				},
			},
		},
	})

	c.Check(model.SystemUserAuthority(), HasLen, 0)
	c.Check(model.SerialAuthority(), DeepEquals, []string{"brand-id1"})
	c.Check(model.PreseedAuthority(), DeepEquals, []string{"brand-id1"})
}

func (mods *modelSuite) TestDecodeWithComponentsBadPresence1(c *C) {
	encoded := strings.Replace(coreModelWithComponentsExample, "TSLINE", mods.tsLine, 1)
	encoded = strings.Replace(encoded, "OTHER", `  -
    name: somesnap
    id: somesnapidididididididididididid
    type: app
    presence: required
    components:
      comp1: badpresenceval
`, 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err.Error(), Equals, `assertion model: presence of component "comp1" of snap "somesnap" must be one of required|optional`)
	c.Assert(a, IsNil)
}

func (mods *modelSuite) TestDecodeWithComponentsBadPresence2(c *C) {
	encoded := strings.Replace(coreModelWithComponentsExample, "TSLINE", mods.tsLine, 1)
	encoded = strings.Replace(encoded, "OTHER", `  -
    name: somesnap
    id: somesnapidididididididididididid
    type: app
    presence: required
    components:
      comp1:
        presence: badpresenceval
`, 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err.Error(), Equals, `assertion model: presence of component "comp1" of snap "somesnap" must be one of required|optional`)
	c.Assert(a, IsNil)
}

func (mods *modelSuite) TestDecodeWithComponentsBadMode(c *C) {
	encoded := strings.Replace(coreModelWithComponentsExample, "TSLINE", mods.tsLine, 1)
	encoded = strings.Replace(encoded, "OTHER", `  -
    name: somesnap
    id: somesnapidididididididididididid
    type: app
    presence: required
    modes:
      - run
    components:
      comp1:
        presence: required
        modes:
          - ephemeral
`, 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err.Error(), Equals, `assertion model: mode "ephemeral" of component "comp1" of snap "somesnap" is incompatible with the snap modes`)
	c.Assert(a, IsNil)
}

func (mods *modelSuite) TestDecodeWithComponentsBadContent(c *C) {
	for i, tc := range []struct {
		compsEntry string
		errMsg     string
	}{
		{`    components:
      - comp1
      - comp2
`,
			`assertion model: "components" of snap "somesnap" must be a map from strings to components`},
		{`    components:
      comp_1: required
`,
			`parsing assertion headers: invalid map entry key: "comp_1"`},
		{`    components:
      comp1:
        presence: required
        other: something
`,
			`assertion model: entry "other" of component "comp1" of snap "somesnap" is unknown`},
		{`    components:
      comp1:
        modes:
          - run
`,
			`assertion model: "presence" of component "comp1" of snap "somesnap" is mandatory`,
		},
		{`    components:
      comp1:
        presence: required
        modes:
          - foomode
`,
			`assertion model: mode "foomode" of component "comp1" of snap "somesnap" is incompatible with the snap modes`,
		},
	} {
		c.Logf("test %d: %q", i, tc.compsEntry)
		encoded := strings.Replace(coreModelWithComponentsExample, "TSLINE", mods.tsLine, 1)
		encoded = strings.Replace(encoded, "OTHER", `  -
    name: somesnap
    id: somesnapidididididididididididid
    type: app
    presence: required
    modes:
      - run
`+tc.compsEntry, 1)
		a, err := asserts.Decode([]byte(encoded))
		c.Assert(err.Error(), Equals, tc.errMsg)
		c.Assert(a, IsNil)
	}
}
