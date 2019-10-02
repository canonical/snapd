// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2019 Canonical Ltd
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
		sysUserAuths +
		reqSnaps +
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
    track: 20
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
		Name:           "brand-gadget",
		SnapType:       "gadget",
		Modes:          []string{"run"},
		DefaultChannel: "stable",
		Presence:       "required",
	})
	c.Check(model.Gadget(), Equals, "brand-gadget")
	c.Check(model.GadgetTrack(), Equals, "")
	c.Check(model.KernelSnap(), DeepEquals, &asserts.ModelSnap{
		Name:           "baz-linux",
		SnapType:       "kernel",
		Modes:          []string{"run"},
		DefaultChannel: "stable",
		Presence:       "required",
	})
	c.Check(model.Kernel(), Equals, "baz-linux")
	c.Check(model.KernelTrack(), Equals, "")
	c.Check(model.Base(), Equals, "core18")
	c.Check(model.BaseSnap(), DeepEquals, &asserts.ModelSnap{
		Name:           "core18",
		SnapType:       "base",
		Modes:          []string{"run"},
		DefaultChannel: "stable",
		Presence:       "required",
	})
	c.Check(model.Store(), Equals, "brand-store")
	c.Check(model.Grade(), Equals, asserts.ModelGradeUnset)
	allSnaps := model.AllSnaps()
	c.Check(allSnaps, DeepEquals, []*asserts.ModelSnap{
		model.BaseSnap(),
		model.GadgetSnap(),
		model.KernelSnap(),
		{
			Name:           "foo",
			Modes:          []string{"run"},
			DefaultChannel: "stable",
			Presence:       "required",
		},
		{
			Name:           "bar",
			Modes:          []string{"run"},
			DefaultChannel: "stable",
			Presence:       "required",
		},
	})
	// essential snaps included
	reqSnaps := model.RequiredWithEssentialSnaps()
	c.Check(reqSnaps, HasLen, len(allSnaps))
	for i, r := range reqSnaps {
		c.Check(r.SnapName(), Equals, allSnaps[i].Name)
		c.Check(r.ID(), Equals, "")
	}
	// essential snaps excluded
	c.Check(model.RequiredNoEssentialSnaps(), DeepEquals, reqSnaps[3:])
	c.Check(model.SystemUserAuthority(), HasLen, 0)
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
	c.Check(model.SystemUserAuthority(), DeepEquals, []string{"foo", "bar"})
}

func (mods *modelSuite) TestDecodeKernelTrack(c *C) {
	withTimestamp := strings.Replace(modelExample, "TSLINE", mods.tsLine, 1)
	encoded := strings.Replace(withTimestamp, "kernel: baz-linux\n", "kernel: baz-linux=18\n", 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	model := a.(*asserts.Model)
	c.Check(model.KernelSnap(), DeepEquals, &asserts.ModelSnap{
		Name:     "baz-linux",
		SnapType: "kernel",
		Modes:    []string{"run"},
		Track:    "18",
		Presence: "required",
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
		Name:     "brand-gadget",
		SnapType: "gadget",
		Modes:    []string{"run"},
		Track:    "18",
		Presence: "required",
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
		{sysUserAuths, "system-user-authority:\n  a: 1\n", `"system-user-authority" header must be '\*' or a list of account ids`},
		{sysUserAuths, "system-user-authority:\n  - 5_6\n", `"system-user-authority" header must be '\*' or a list of account ids`},
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
	c.Assert(err, ErrorMatches, `model assertion timestamp outside of signing key validity \(key valid since.*\)`)
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
	c.Check(model.Architecture(), Equals, "amd64")
	c.Check(model.GadgetSnap(), DeepEquals, &asserts.ModelSnap{
		Name:           "brand-gadget",
		SnapType:       "gadget",
		Modes:          []string{"run"},
		DefaultChannel: "stable",
		Presence:       "required",
	})
	c.Check(model.Gadget(), Equals, "brand-gadget")
	c.Check(model.KernelSnap(), IsNil)
	c.Check(model.Kernel(), Equals, "")
	c.Check(model.KernelTrack(), Equals, "")
	c.Check(model.Base(), Equals, "")
	c.Check(model.BaseSnap(), IsNil)
	c.Check(model.Store(), Equals, "brand-store")
	allSnaps := model.AllSnaps()
	c.Check(allSnaps, DeepEquals, []*asserts.ModelSnap{
		model.GadgetSnap(),
		{
			Name:           "foo",
			Modes:          []string{"run"},
			DefaultChannel: "stable",
			Presence:       "required",
		},
		{
			Name:           "bar",
			Modes:          []string{"run"},
			DefaultChannel: "stable",
			Presence:       "required",
		},
	})
	// gadget included
	reqSnaps := model.RequiredWithEssentialSnaps()
	c.Check(reqSnaps, HasLen, len(allSnaps))
	for i, r := range reqSnaps {
		c.Check(r.SnapName(), Equals, allSnaps[i].Name)
		c.Check(r.ID(), Equals, "")
	}
	// gadget excluded
	c.Check(model.RequiredNoEssentialSnaps(), DeepEquals, reqSnaps[1:])
}

func (mods *modelSuite) TestClassicDecodeInvalid(c *C) {
	encoded := strings.Replace(classicModelExample, "TSLINE", mods.tsLine, 1)

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"classic: true\n", "classic: foo\n", `"classic" header must be 'true' or 'false'`},
		{"architecture: amd64\n", "architecture:\n  - foo\n", `"architecture" header must be a string`},
		{"gadget: brand-gadget\n", "gadget:\n  - foo\n", `"gadget" header must be a string`},
		{"gadget: brand-gadget\n", "kernel: brand-kernel\n", `cannot specify a kernel with a classic model`},
		{"gadget: brand-gadget\n", "base: some-base\n", `cannot specify a base with a classic model`},
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
	c.Check(model.Architecture(), Equals, "")
	c.Check(model.GadgetSnap(), IsNil)
	c.Check(model.Gadget(), Equals, "")
	c.Check(model.GadgetTrack(), Equals, "")
}

func (mods *modelSuite) TestCore20DecodeOK(c *C) {
	encoded := strings.Replace(core20ModelExample, "TSLINE", mods.tsLine, 1)
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
		DefaultChannel: "stable",
		Presence:       "required",
	})
	c.Check(model.Gadget(), Equals, "brand-gadget")
	c.Check(model.GadgetTrack(), Equals, "")
	c.Check(model.KernelSnap(), DeepEquals, &asserts.ModelSnap{
		Name:     "baz-linux",
		SnapID:   "bazlinuxidididididididididididid",
		SnapType: "kernel",
		Modes:    []string{"run", "ephemeral"},
		Track:    "20",
		Presence: "required",
	})
	c.Check(model.Kernel(), Equals, "baz-linux")
	c.Check(model.KernelTrack(), Equals, "20")
	c.Check(model.Base(), Equals, "core20")
	c.Check(model.BaseSnap(), DeepEquals, &asserts.ModelSnap{
		Name:           "core20",
		SnapType:       "base",
		Modes:          []string{"run", "ephemeral"},
		DefaultChannel: "stable",
		Presence:       "required",
	})
	c.Check(model.Store(), Equals, "brand-store")
	c.Check(model.Grade(), Equals, asserts.ModelSecured)
	allSnaps := model.AllSnaps()
	c.Check(allSnaps, DeepEquals, []*asserts.ModelSnap{
		model.BaseSnap(),
		model.GadgetSnap(),
		model.KernelSnap(),
		{
			Name:           "other-base",
			SnapID:         "otherbasedididididididididididid",
			SnapType:       "base",
			Modes:          []string{"run"},
			DefaultChannel: "stable",
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
			DefaultChannel: "stable",
			Presence:       "optional",
		},
	})
	// essential snaps included
	reqSnaps := model.RequiredWithEssentialSnaps()
	c.Check(reqSnaps, HasLen, len(allSnaps)-1)
	for i, r := range reqSnaps {
		c.Check(r.SnapName(), Equals, allSnaps[i].Name)
		c.Check(r.ID(), Equals, allSnaps[i].SnapID)
	}
	// essential snaps excluded
	c.Check(model.RequiredNoEssentialSnaps(), DeepEquals, reqSnaps[3:])
	c.Check(model.SystemUserAuthority(), HasLen, 0)
}

func (mods *modelSuite) TestCore20ExplictBootBase(c *C) {
	encoded := strings.Replace(core20ModelExample, "TSLINE", mods.tsLine, 1)
	encoded = strings.Replace(encoded, "OTHER", `  -
    name: core20
    id: core20ididididididididididididid
    type: base
    default-channel: candidate
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
		DefaultChannel: "candidate",
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

func (mods *modelSuite) TestCore20GradeDangerous(c *C) {
	encoded := strings.Replace(core20ModelExample, "TSLINE", mods.tsLine, 1)
	encoded = strings.Replace(encoded, "OTHER", "", 1)
	encoded = strings.Replace(encoded, "grade: secured\n", "grade: dangerous\n", 1)
	// snap ids are optional with grade dangerous to allow working
	// with local/not pushed yet to the store snaps
	encoded = strings.Replace(encoded, "    id: myappdididididididididididididid\n", "", 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.ModelType)
	model := a.(*asserts.Model)
	c.Check(model.Grade(), Equals, asserts.ModelDangerous)
	allSnaps := model.AllSnaps()
	c.Check(allSnaps[len(allSnaps)-2], DeepEquals, &asserts.ModelSnap{
		Name:           "myapp",
		SnapType:       "app",
		Modes:          []string{"run"},
		DefaultChannel: "2.0",
		Presence:       "required",
	})
}

func (mods *modelSuite) TestCore20DecodeInvalid(c *C) {
	encoded := strings.Replace(core20ModelExample, "TSLINE", mods.tsLine, 1)

	snapsStanza := encoded[strings.Index(encoded, "snaps:"):strings.Index(encoded, "grade:")]

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"base: core20\n", "", `"base" header is mandatory`},
		{"OTHER", "classic: true\n", `cannot use extended snaps header for a classic model \(yet\)`},
		{snapsStanza, "snaps: snap\n", `"snaps" header must be a list of maps`},
		{snapsStanza, "snaps:\n  - snap\n", `"snaps" header must be a list of maps`},
		{"name: myapp\n", "other: 1\n", `"name" of snap is mandatory`},
		{"name: myapp\n", "name: myapp_2\n", `invalid snap name "myapp_2"`},
		{"id: myappdididididididididididididid\n", "id: 2\n", `"id" of snap "myapp" contains invalid characters: "2"`},
		{"    id: myappdididididididididididididid\n", "", `"id" of snap "myapp" is mandatory for stable model`},
		{"type: gadget\n", "type:\n      - g\n", `"type" of snap "brand-gadget" must be a string`},
		{"type: app\n", "type: thing\n", `"type" of snap "myappopt" must be one of must be one of app|base|gadget|kernel|core`},
		{"modes:\n      - run\n", "modes: run\n", `"modes" of snap "other-base" must be a list of strings`},
		{"track: 20\n", "track:\n      - x\n", `"track" of snap "baz-linux" must be a string`},
		{"track: 20\n", "track: 20/edge\n", `invalid locked track for snap \"baz-linux\": 20/edge`},
		{"track: 20\n", "track: 20////\n", `invalid locked track for snap \"baz-linux\": 20////`},
		{"default-channel: 2.0\n", "default-channel:\n      - x\n", `"default-channel" of snap "myapp" must be a string`},
		{"default-channel: 2.0\n", "default-channel: 2.0/xyz/z\n", `invalid default channel for snap "myapp": invalid risk in channel name: 2.0/xyz/z`},
		{"track: 20\n", "track: 20\n    default-channel: 20/foo\n", `snap "baz-linux" cannot specify both default channel and locked track`},
		{"presence: optional\n", "presence:\n      - opt\n", `"presence" of snap "myappopt" must be a string`},
		{"presence: optional\n", "presence: no\n", `"presence" of snap "myappopt" must be one of must be one of required|optional`},
		{"OTHER", "  -\n    name: myapp\n    id: myappdididididididididididididid\n", `cannot list the same snap "myapp" multiple times`},
		{"OTHER", "  -\n    name: myapp2\n    id: myappdididididididididididididid\n", `cannot specify the same snap id "myappdididididididididididididid" multiple times, specified for snaps "myapp" and "myapp2"`},
		{"OTHER", "  -\n    name: kernel2\n    id: kernel2didididididididididididid\n    type: kernel\n", `cannot specify multiple kernel snaps: "baz-linux" and "kernel2"`},
		{"OTHER", "  -\n    name: gadget2\n    id: gadget2didididididididididididid\n    type: gadget\n", `cannot specify multiple gadget snaps: "brand-gadget" and "gadget2"`},
		{"type: gadget\n", "type: gadget\n    presence: required\n", `essential snaps are always available, cannot specify modes or presence for snap "brand-gadget"`},
		{"type: gadget\n", "type: gadget\n    modes:\n      - run\n", `essential snaps are always available, cannot specify modes or presence for snap "brand-gadget"`},
		{"type: kernel\n", "type: kernel\n    presence: required\n", `essential snaps are always available, cannot specify modes or presence for snap "baz-linux"`},
		{"OTHER", "  -\n    name: core20\n    id: core20ididididididididididididid\n    type: base\n    presence: optional\n", `essential snaps are always available, cannot specify modes or presence for snap "core20"`},
		{"type: gadget\n", "type: app\n", `one "snaps" header entry must specify the model gadget`},
		{"type: kernel\n", "type: app\n", `one "snaps" header entry must specify the model kernel`},
		{"OTHER", "  -\n    name: core20\n    id: core20ididididididididididididid\n    type: app\n", `boot base "core20" must specify type "base", not "app"`},
		{"OTHER", "kernel: foo\n", `cannot specify separate "kernel" header once using the extended snaps header`},
		{"OTHER", "gadget: foo\n", `cannot specify separate "gadget" header once using the extended snaps header`},
		{"OTHER", "required-snaps:\n  - foo\n", `cannot specify separate "required-snaps" header once using the extended snaps header`},
		{"grade: secured\n", "grade: foo\n", `grade for model must be secured|signed|dangerous`},
	}
	for _, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		invalid = strings.Replace(invalid, "OTHER", "", 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, modelErrPrefix+test.expectedErr)
	}
}
