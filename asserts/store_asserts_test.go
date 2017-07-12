package asserts_test

import (
	"fmt"
	"strings"
	"time"

	"github.com/snapcore/snapd/asserts"
	. "gopkg.in/check.v1"
)

var _ = Suite(&storeSuite{})

type storeSuite struct {
	validExample string
}

func (s *storeSuite) SetUpSuite(c *C) {
	s.validExample = "type: store\n" +
		"authority-id: canonical\n" +
		"operator-id: op-id1\n" +
		"store: store1\n" +
		"address: https://store.example.com\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij\n" +
		"\n" +
		"AXNpZw=="
}

func (s *storeSuite) TestDecodeOK(c *C) {
	a, err := asserts.Decode([]byte(s.validExample))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.StoreType)
	store := a.(*asserts.Store)

	c.Check(store.OperatorID(), Equals, "op-id1")
	c.Check(store.Store(), Equals, "store1")
	c.Check(store.Address().String(), Equals, "https://store.example.com")
}

var storeErrPrefix = "assertion store: "

func (s *storeSuite) TestDecodeInvalidHeaders(c *C) {
	tests := []struct{ original, invalid, expectedErr string }{
		{"operator-id: op-id1\n", "", `"operator-id" header is mandatory`},
		{"operator-id: op-id1\n", "operator-id: \n", `"operator-id" header should not be empty`},
		{"store: store1\n", "", `"store" header is mandatory`},
		{"store: store1\n", "store: \n", `"store" header should not be empty`},
		{"address: https://store.example.com\n", "", `"address" header is mandatory`},
		{"address: https://store.example.com\n", "address: \n", `"address" header should not be empty`},
	}

	for _, test := range tests {
		invalid := strings.Replace(s.validExample, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, storeErrPrefix+test.expectedErr)
	}
}

func (s *storeSuite) TestAddress(c *C) {
	tests := []struct {
		address string
		err     string
	}{
		// Valid addresses.
		{"http://example.com/", ""},
		{"https://example.com/", ""},
		{"https://example.com/some/path/", ""},
		{"https://example.com:443/", ""},
		{"https://example.com:1234/", ""},
		{"https://user:pass@example.com/", ""},
		{"https://token@example.com/", ""},

		// Invalid addresses.
		{"://example.com", `"address" header must be a valid URL`},
		{"example.com", `"address" header scheme must be "https" or "http"`},
		{"//example.com", `"address" header scheme must be "https" or "http"`},
		{"ftp://example.com", `"address" header scheme must be "https" or "http"`},
		{"mailto:someone@example.com", `"address" header scheme must be "https" or "http"`},
		{"https://", `"address" header must have a host`},
		{"https:///", `"address" header must have a host`},
		{"https:///some/path", `"address" header must have a host`},
		{"https://example.com/?foo=bar", `"address" header must not have a query`},
		{"https://example.com/#fragment", `"address" header must not have a fragment`},
	}

	for _, test := range tests {
		encoded := strings.Replace(
			s.validExample, "address: https://store.example.com\n",
			fmt.Sprintf("address: %s\n", test.address), 1)
		assert, err := asserts.Decode([]byte(encoded))
		if test.err != "" {
			c.Assert(err, NotNil)
			c.Check(err.Error(), Equals, storeErrPrefix+test.err+": "+test.address)
		} else {
			c.Assert(err, IsNil)
			c.Check(assert.(*asserts.Store).Address().String(), Equals, test.address)
		}
	}
}

func (s *storeSuite) TestCheckAuthority(c *C) {
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

	storeHeaders := map[string]interface{}{
		"operator-id": operator.HeaderString("account-id"),
		"store":       "store1",
		"address":     "https://store.example.com",
	}

	// store signed by some other account fails.
	otherDB := setup3rdPartySigning(c, "other", storeDB, db)
	store, err := otherDB.Sign(asserts.StoreType, storeHeaders, nil, "")
	c.Assert(err, IsNil)
	err = db.Check(store)
	c.Assert(err, ErrorMatches, `store assertion for operator-id "op-id1" and store "store1" is not signed by a directly trusted authority: other`)

	// but succeeds when signed by a trusted authority.
	store, err = storeDB.Sign(asserts.StoreType, storeHeaders, nil, "")
	c.Assert(err, IsNil)
	err = db.Check(store)
	c.Assert(err, IsNil)
}

func (s *storeSuite) TestCheckOperatorAccount(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	assert, err := storeDB.Sign(asserts.StoreType, map[string]interface{}{
		"operator-id": "op-id1",
		"store":       "store1",
		"address":     "https://store.example.com",
	}, nil, "")
	c.Assert(err, IsNil)

	// No account for operator op-id1 yet, so Check fails.
	err = db.Check(assert)
	c.Assert(err, ErrorMatches, `store assertion for operator-id "op-id1" and store "store1" does not have a matching account assertion for the operator "op-id1"`)

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

func (s *storeSuite) TestPrerequisites(c *C) {
	assert, err := asserts.Decode([]byte(s.validExample))
	c.Assert(err, IsNil)
	c.Assert(assert.Prerequisites(), DeepEquals, []*asserts.Ref{
		{Type: asserts.AccountType, PrimaryKey: []string{"op-id1"}},
	})
}
