/*
 * Copyright (C) 2016-2018 Canonical Ltd
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

package ifacestate

import (
	"strings"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/ifacestate/udevmonitor"
	"github.com/snapcore/snapd/overlord/state"

	"gopkg.in/check.v1"
)

var (
	AddImplicitSlots             = addImplicitSlots
	SnapsWithSecurityProfiles    = snapsWithSecurityProfiles
	CheckAutoconnectConflicts    = checkAutoconnectConflicts
	FindSymmetricAutoconnectTask = findSymmetricAutoconnectTask
	ConnectPriv                  = connect
	GetConns                     = getConns
	SetConns                     = setConns
	DefaultDeviceKey             = defaultDeviceKey
	MakeSlotName                 = makeSlotName
	EnsureUniqueName             = ensureUniqueName
	SuggestedSlotName            = suggestedSlotName
	InSameChangeWaitChain        = inSameChangeWaitChain
)

func NewConnectOptsWithAutoSet() connectOpts {
	return connectOpts{AutoConnect: true, ByGadget: false}
}

func MockRemoveStaleConnections(f func(st *state.State) error) (restore func()) {
	old := removeStaleConnections
	removeStaleConnections = f
	return func() { removeStaleConnections = old }
}

func MockContentLinkRetryTimeout(d time.Duration) (restore func()) {
	old := contentLinkRetryTimeout
	contentLinkRetryTimeout = d
	return func() { contentLinkRetryTimeout = old }
}

func MockCreateUDevMonitor(new func(udevmonitor.DeviceAddedFunc, udevmonitor.DeviceRemovedFunc) udevmonitor.Interface) (restore func()) {
	old := createUDevMonitor
	createUDevMonitor = new
	return func() {
		createUDevMonitor = old
	}
}

func MockUDevInitRetryTimeout(t time.Duration) (restore func()) {
	old := udevInitRetryTimeout
	udevInitRetryTimeout = t
	return func() {
		udevInitRetryTimeout = old
	}
}

// UpperCaseConnState returns a canned connection state map.
// This allows us to keep connState private and still write some tests for it.
func UpperCaseConnState() map[string]connState {
	return map[string]connState{
		"APP:network CORE:network": {Auto: true, Interface: "network"},
	}
}

type AssertsMock struct {
	Db           *asserts.Database
	storeSigning *assertstest.StoreStack
	brandSigning *assertstest.SigningDB
}

func (am *AssertsMock) MockAsserts(c *check.C, st *state.State) {
	am.storeSigning = assertstest.NewStoreStack("canonical", nil)
	brandPrivKey, _ := assertstest.GenerateKey(752)
	am.brandSigning = assertstest.NewSigningDB("my-brand", brandPrivKey)

	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   am.storeSigning.Trusted,
	})
	c.Assert(err, check.IsNil)
	am.Db = db
	err = db.Add(am.storeSigning.StoreAccountKey(""))
	c.Assert(err, check.IsNil)

	st.Lock()
	assertstate.ReplaceDB(st, am.Db)

	brandAcct := assertstest.NewAccount(am.storeSigning, "my-brand", map[string]interface{}{
		"account-id": "my-brand",
	}, "")
	err = assertstate.Add(st, brandAcct)
	c.Assert(err, check.IsNil)

	brandPubKey, err := am.brandSigning.PublicKey("")
	c.Assert(err, check.IsNil)
	brandAccKey := assertstest.NewAccountKey(am.storeSigning, brandAcct, nil, brandPubKey, "")
	err = assertstate.Add(st, brandAccKey)
	c.Assert(err, check.IsNil)
	st.Unlock()
}

func (am *AssertsMock) MockModel(c *check.C, st *state.State, extraHeaders map[string]interface{}) {
	headers := map[string]interface{}{
		"series":       "16",
		"brand-id":     "my-brand",
		"model":        "my-model",
		"gadget":       "gadget",
		"kernel":       "krnl",
		"architecture": "amd64",
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	for k, v := range extraHeaders {
		headers[k] = v
	}
	model, err := am.brandSigning.Sign(asserts.ModelType, headers, nil, "")
	c.Assert(err, check.IsNil)
	st.Lock()
	defer st.Unlock()
	err = assertstate.Add(st, model)
	c.Assert(err, check.IsNil)
	err = auth.SetDevice(st, &auth.DeviceState{
		Brand: "my-brand",
		Model: "my-model",
	})
	c.Assert(err, check.IsNil)
}

func (am *AssertsMock) MockSnapDecl(c *check.C, name, publisher string, extraHeaders map[string]interface{}) {
	_, err := am.Db.Find(asserts.AccountType, map[string]string{
		"account-id": publisher,
	})
	if asserts.IsNotFound(err) {
		acct := assertstest.NewAccount(am.storeSigning, publisher, map[string]interface{}{
			"account-id": publisher,
		}, "")
		err = am.Db.Add(acct)
	}
	c.Assert(err, check.IsNil)

	headers := map[string]interface{}{
		"series":       "16",
		"snap-name":    name,
		"publisher-id": publisher,
		"snap-id":      (name + strings.Repeat("id", 16))[:32],
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	for k, v := range extraHeaders {
		headers[k] = v
	}

	snapDecl, err := am.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, check.IsNil)

	err = am.Db.Add(snapDecl)
	c.Assert(err, check.IsNil)
}

func (am *AssertsMock) MockStore(c *check.C, st *state.State, storeID string, extraHeaders map[string]interface{}) {
	headers := map[string]interface{}{
		"store":       storeID,
		"operator-id": am.storeSigning.AuthorityID,
		"timestamp":   time.Now().Format(time.RFC3339),
	}
	for k, v := range extraHeaders {
		headers[k] = v
	}
	storeAs, err := am.storeSigning.Sign(asserts.StoreType, headers, nil, "")
	c.Assert(err, check.IsNil)
	st.Lock()
	defer st.Unlock()
	err = assertstate.Add(st, storeAs)
	c.Assert(err, check.IsNil)
}
