package asserts_test

import (
	"strings"
	"time"

	"github.com/snapcore/snapd/asserts"
	. "gopkg.in/check.v1"
)

var _ = Suite(&enterpriseStoreSuite{})

type enterpriseStoreSuite struct {
	exampleAddress string
	validExample   string
}

func (estores *enterpriseStoreSuite) SetUpSuite(c *C) {
	estores.exampleAddress = "address:\n  - foo.example.com\n  - bar.example.com\n"
	estores.validExample = "type: enterprise-store\n" +
		"authority-id: canonical\n" +
		"operator-id: op-id1\n" +
		"store: store1\n" +
		estores.exampleAddress +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij\n" +
		"\n" +
		"AXNpZw=="
}

func (estores *enterpriseStoreSuite) TestDecodeOK(c *C) {
	a, err := asserts.Decode([]byte(estores.validExample))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.EnterpriseStoreType)
	estore := a.(*asserts.EnterpriseStore)

	c.Check(estore.OperatorID(), Equals, "op-id1")
	c.Check(estore.Store(), Equals, "store1")
	c.Check(estore.Address(), DeepEquals, []string{"foo.example.com", "bar.example.com"})
}

var eStoreErrPrefix = "assertion enterprise-store: "

func (estores *enterpriseStoreSuite) TestDecodeInvalidHeaders(c *C) {
	tests := []struct{ original, invalid, expectedErr string }{
		{"operator-id: op-id1\n", "", `"operator-id" header is mandatory`},
		{"operator-id: op-id1\n", "operator-id: \n", `"operator-id" header should not be empty`},
		{"store: store1\n", "", `"store" header is mandatory`},
		{"store: store1\n", "store: \n", `"store" header should not be empty`},
		{estores.exampleAddress, "", `"address" header is mandatory`},
		{estores.exampleAddress, "address: foo\n", `"address" header must be a list of strings`},
		{estores.exampleAddress, "address:\n  -\n    - foo\n", `"address" header must be a list of strings`},
	}

	for _, test := range tests {
		invalid := strings.Replace(estores.validExample, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, eStoreErrPrefix+test.expectedErr)
	}
}

func (estores *enterpriseStoreSuite) TestCheckAuthority(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	// Add account for operator.
	operator, err := storeDB.Sign(asserts.AccountType, map[string]interface{}{
		"account-id":   "op-id1",
		"display-name": "op-id1",
		"validation":   "unknown",
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = db.Add(operator)
	c.Assert(err, IsNil)

	estoreHeaders := map[string]interface{}{
		"operator-id": operator.HeaderString("account-id"),
		"store":       "store1",
		"address":     []interface{}{"foo.example.com"},
	}

	// enterprise-store signed by some other account fails.
	otherDB := setup3rdPartySigning(c, "other", storeDB, db)
	estore, err := otherDB.Sign(asserts.EnterpriseStoreType, estoreHeaders, nil, "")
	c.Assert(err, IsNil)
	err = db.Check(estore)
	c.Assert(err, ErrorMatches, `enterprise-store assertion for operator-id "op-id1" and store "store1" is not signed by a directly trusted authority: other`)

	// but succeeds when signed by a trusted authority.
	estore, err = storeDB.Sign(asserts.EnterpriseStoreType, estoreHeaders, nil, "")
	c.Assert(err, IsNil)
	err = db.Check(estore)
	c.Assert(err, IsNil)
}

func (estores *enterpriseStoreSuite) TestCheckOperatorAccount(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	assert, err := storeDB.Sign(asserts.EnterpriseStoreType, map[string]interface{}{
		"operator-id": "op-id1",
		"store":       "store1",
		"address":     []interface{}{"foo.example.com"},
	}, nil, "")
	c.Assert(err, IsNil)

	// No account for operator op-id1 yet, so Check fails.
	err = db.Check(assert)
	c.Assert(err, ErrorMatches, `enterprise-store assertion for operator-id "op-id1" and store "store1" does not have a matching account assertion for the operator "op-id1"`)

	// Add the op-id1 account.
	assert, err = storeDB.Sign(asserts.AccountType, map[string]interface{}{
		"account-id":   "op-id1",
		"display-name": "op-id1",
		"validation":   "unknown",
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = db.Add(assert)
	c.Assert(err, IsNil)

	// Now the operator exists so Check succeeds.
	err = db.Check(assert)
	c.Assert(err, IsNil)
}

func (estores *enterpriseStoreSuite) TestPrerequisites(c *C) {
	assert, err := asserts.Decode([]byte(estores.validExample))
	c.Assert(err, IsNil)
	c.Assert(assert.Prerequisites(), DeepEquals, []*asserts.Ref{
		{Type: asserts.AccountType, PrimaryKey: []string{"op-id1"}},
	})
}
