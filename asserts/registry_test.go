// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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
 */

package asserts_test

import (
	"fmt"
	"sort"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
)

type registrySuite struct{}

var _ = Suite(&registrySuite{})

type externalAssertion struct {
	asserts.AssertionBase
}

var _ asserts.ConsistencyChecker = (*externalAssertion)(nil)

func assembleExternal(assert asserts.AssertionBase) (asserts.Assertion, error) {
	if assert.HeaderString("external-id") == "" {
		return nil, fmt.Errorf(`"external-id" header is mandatory`)
	}
	return &externalAssertion{AssertionBase: assert}, nil
}

func (a *externalAssertion) CheckConsistency(asserts.RODatabase, *asserts.AccountKey) error {
	return fmt.Errorf("external consistency check")
}

func mustNewType(c *C, definition asserts.TypeDefinition) *asserts.AssertionType {
	assertionType, err := asserts.NewAssertionType(definition)
	c.Assert(err, IsNil)
	return assertionType
}

func newExternalType(c *C, name string) *asserts.AssertionType {
	return mustNewType(c, asserts.TypeDefinition{
		Name:       name,
		PrimaryKey: []string{"external-id"},
		Assembler:  assembleExternal,
	})
}

func configureTypes(c *C, assertionTypes ...*asserts.AssertionType) func() {
	restore := asserts.MockRegistryConfiguration()
	c.Assert(asserts.ConfigureTypes(assertionTypes...), IsNil)
	return restore
}

func (s *registrySuite) TestNewAssertionType(c *C) {
	primaryKey := []string{"external-id"}
	assertionType, err := asserts.NewAssertionType(asserts.TypeDefinition{
		Name:       "external",
		PrimaryKey: primaryKey,
		Assembler:  assembleExternal,
	})
	c.Assert(err, IsNil)
	c.Check(assertionType.Name, Equals, "external")
	c.Check(assertionType.PrimaryKey, DeepEquals, []string{"external-id"})

	primaryKey[0] = "changed"
	c.Check(assertionType.PrimaryKey, DeepEquals, []string{"external-id"})
}

func (s *registrySuite) TestNewAssertionTypeValidation(c *C) {
	validAssembler := assembleExternal
	tests := []struct {
		definition asserts.TypeDefinition
		error      string
	}{
		{asserts.TypeDefinition{Assembler: validAssembler}, "assertion type name cannot be empty"},
		{asserts.TypeDefinition{Name: "../external", Assembler: validAssembler}, `invalid assertion type name: "\.\./external"`},
		{asserts.TypeDefinition{Name: "external"}, `assertion type "external" assembler cannot be nil`},
		{asserts.TypeDefinition{
			Name: "external", PrimaryKey: []string{"invalid/name"}, Assembler: validAssembler,
		}, `assertion type "external" has invalid primary key header name "invalid/name"`},
		{asserts.TypeDefinition{
			Name: "external", PrimaryKey: []string{"id", "id"}, Assembler: validAssembler,
		}, `assertion type "external" has duplicate primary key header "id"`},
		{asserts.TypeDefinition{
			Name: "external", PrimaryKey: []string{"type"}, Assembler: validAssembler,
		}, `assertion type "external" cannot use meta header "type" as primary key`},
	}
	for _, test := range tests {
		assertionType, err := asserts.NewAssertionType(test.definition)
		c.Check(assertionType, IsNil)
		c.Check(err, ErrorMatches, test.error)
	}
}

func (s *registrySuite) TestConfigureTypesIsAtomicAndRetryable(c *C) {
	restore := asserts.MockRegistryConfiguration()
	defer restore()
	c.Check(asserts.Type("account"), Equals, asserts.AccountType)

	externalType := newExternalType(c, "external-atomic")

	err := asserts.ConfigureTypes()
	c.Check(err, ErrorMatches, "cannot configure assertion types: no assertion types provided")
	err = asserts.ConfigureTypes(nil)
	c.Check(err, ErrorMatches, "cannot configure assertion types: assertion type cannot be nil")

	externalType.Name = "../renamed"
	err = asserts.ConfigureTypes(externalType)
	c.Check(err, ErrorMatches, `cannot configure assertion types: invalid assertion type name: "\.\./renamed"`)
	externalType.Name = "external-atomic"

	err = asserts.ConfigureTypes(externalType, asserts.AccountType)
	c.Check(err, ErrorMatches, `cannot configure assertion types: assertion type "account" is already registered`)
	c.Check(asserts.Type("external-atomic"), IsNil)

	err = asserts.ConfigureTypes(externalType)
	c.Assert(err, IsNil)
	c.Check(asserts.Type("external-atomic"), Equals, externalType)
	c.Check(asserts.Type("account"), Equals, asserts.AccountType)

	err = asserts.ConfigureTypes(newExternalType(c, "another-external"))
	c.Check(err, ErrorMatches, "assertion types are already configured")
}

func (s *registrySuite) TestConfiguredTypeUsesExistingPaths(c *C) {
	restore := asserts.MockRegistryConfiguration()
	defer restore()

	externalType := newExternalType(c, "external-end-to-end")
	err := asserts.ConfigureTypes(externalType)
	c.Assert(err, IsNil)

	c.Check(asserts.Type("external-end-to-end"), Equals, externalType)
	names := asserts.TypeNames()
	pos := sort.SearchStrings(names, externalType.Name)
	c.Check(pos < len(names) && names[pos] == externalType.Name, Equals, true)
	c.Check(asserts.MaxSupportedFormats(0)[externalType.Name], Equals, 0)

	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{Backstore: asserts.NewMemoryBackstore()})
	c.Assert(err, IsNil)
	err = db.ImportKey(testPrivKey1)
	c.Assert(err, IsNil)

	a, err := db.Sign(externalType, map[string]any{
		"authority-id": "external-authority",
		"external-id":  "one",
	}, nil, testPrivKey1.PublicKey().ID())
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, externalType)
	c.Check(a.(*externalAssertion).HeaderString("external-id"), Equals, "one")
	c.Check(asserts.CheckCrossConsistency(a, nil, db, time.Time{}, time.Time{}), ErrorMatches, "external consistency check")

	encoded := asserts.Encode(a)
	decoded, err := asserts.Decode(encoded)
	c.Assert(err, IsNil)
	c.Check(decoded.Type(), Equals, externalType)

	backstore, err := asserts.OpenFSBackstore(c.MkDir())
	c.Assert(err, IsNil)
	err = backstore.Put(externalType, a)
	c.Assert(err, IsNil)
	fromDisk, err := backstore.Get(externalType, []string{"one"}, externalType.MaxSupportedFormat())
	c.Assert(err, IsNil)
	c.Check(fromDisk.Type(), Equals, externalType)
}
