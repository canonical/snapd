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
 *
 */

package daemon

// Mediated assertion signing for restricted snaps.
//
// A snap that plugs the "assertion-sign" interface and runs unprivileged can
// ask snapd to sign an assertion on its behalf. Authorization is enforced
// twice, independently:
//
//  1. The access checker (interfaceRootAccess) requires a *connected*
//     "assertion-sign" plug for the caller. The caller is identified from peer
//     credentials / AppArmor security tag (never from request headers), and a
//     non-root caller must additionally pass the io.snapcraft.snapd.sign polkit
//     action (auth_admin). See requireInterfaceApiAccessImpl in daemon/access.go.
//  2. The handler enforces the per-connection "assertion-types" allow-list, so
//     a connection may only sign the assertion types it was granted.
//
// The private key never reaches the calling snap: signing happens in the daemon
// using a keypair manager configured on the daemon (the user's GPG keyring or an
// external key manager via SNAPD_EXT_KEYMGR). See the design doc, §7.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/signtool"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/ifacestate"
)

// maxSignBody limits the assertion statement (headers + optional body) accepted
// by the signing endpoint. Statements are small JSON; the cap is a DoS guard,
// not a functional limit.
const maxSignBody = 1 << 20 // 1 MiB

// signKeypairManager is the daemon's keypair manager. It defaults to
// signtool.GetKeypairManager (GPG keyring, or SNAPD_EXT_KEYMGR when set on the
// daemon unit). Tests override it via MockSignKeypairManager.
var signKeypairManager = signtool.GetKeypairManager

// fetchAccountKeyImpl / fetchAccountImpl perform the store lookups for the
// "chain" option (like `snap sign --chain` does). They are variables so
// tests can exercise the chain without a live store; the production path is
// exactly the daemon's storeFrom(...).Assertion, the same one the CLI uses.
var (
	fetchAccountKeyImpl = fetchAccountKey
	fetchAccountImpl    = fetchAccount
)

var (
	signAssertCmd = &Command{
		Path: "/v2/assertions/sign",
		POST: signAssert,
		// interface + polkit gate: requires an active "assertion-sign" plug for
		// the caller (ucred security tag -> instance name) and, for non-root
		// callers, the polkit action. See daemon/access.go.
		WriteAccess: interfaceRootAccess{
			Interfaces: []string{"assertion-sign"},
			Polkit:     polkitActionSign,
		},
	}

	listKeysCmd = &Command{
		Path: "/v2/keys",
		GET:  listKeys,
		ReadAccess: interfaceOpenAccess{
			Interfaces: []string{"assertion-sign"},
		},
	}
)

// signRequest is the JSON body of POST /v2/assertions/sign.
type signRequest struct {
	// Key is the key name (or "default"). Resolved to a key id by the manager.
	Key string `json:"key"`
	// Statement is the assertion headers plus an optional "body" pseudo-header,
	// matching the `snap sign` stdin format 1:1.
	Statement map[string]any `json:"statement"`
	// Chain also returns the account-key and account assertions needed to
	// validate the signed assertion on any device (like `snap sign --chain`).
	Chain bool `json:"chain"`
	// UpdateTimestamp updates the "timestamp" header to the current time.
	UpdateTimestamp bool `json:"update-timestamp"`
}

func signAssert(c *Command, r *http.Request, user *auth.UserState) Response {
	// The caller's identity and its *connected* assertion-sign plug were already
	// established by the access checker. We re-derive the instance name only to
	// scope the per-connection type allow-list below.
	ucred, err := ucrednetGet(r.Context())
	if err != nil {
		return InternalError("cannot identify caller: %v", err)
	}
	instanceName, err := ucred.InstanceName()
	if err != nil {
		return InternalError("cannot determine caller snap instance: %v", err)
	}

	if r.ContentLength > maxSignBody {
		return BadRequest("request body too large")
	}
	var req signRequest
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&req); err != nil {
		return BadRequest("cannot decode request body: %v", err)
	}
	if req.Statement == nil {
		return BadRequest(`missing "statement"`)
	}
	keyName := req.Key
	if keyName == "" {
		keyName = "default"
	}

	// Per-connection type allow-list (from the plug attribute).
	allowed, err := connectedPlugAssertionTypes(c, instanceName)
	if err != nil {
		return InternalError("%v", err)
	}
	if !allowed.contains(req.Statement) {
		// Deny before any polkit prompt interaction is possible.
		return Forbidden("assertion type %q is not allowed for this connection", req.Statement["type"])
	}

	// Resolve the signing key.
	kpm, err := signKeypairManager()
	if err != nil {
		return InternalError("cannot open keypair manager: %v", err)
	}
	privKey, err := kpm.GetByName(keyName)
	if err != nil {
		return BadRequest("cannot use %q key: %v", keyName, err)
	}
	stmt, err := json.Marshal(req.Statement)
	if err != nil {
		return BadRequest("cannot marshal statement: %v", err)
	}

	// Look up the account-key for the key, like `snap sign` does, to
	// cross-check the statement against the key's signing constraints.
	// With "chain" a lookup failure is an error (the chain is unusable
	// without it); without "chain" a missing account-key is not (the CLI
	// only warns in that case).
	keyID := privKey.PublicKey().ID()
	accountKey, akErr := fetchAccountKeyImpl(c, keyID, user)
	if req.Chain && akErr != nil {
		return BadRequest("cannot create assertion chain: %v", akErr)
	}

	opts := &signtool.Options{
		KeyID:           keyID,
		Statement:       stmt,
		UpdateTimestamp: req.UpdateTimestamp,
	}
	if accountKey != nil {
		opts.AccountKey = accountKey
	}
	encoded, err := signtool.Sign(opts, kpm)
	if err != nil {
		return BadRequest("cannot sign assertion: %v", err)
	}

	// Encode (identical bytes to `snap sign` output).
	var buf bytes.Buffer
	enc := asserts.NewEncoder(&buf)
	if err := enc.WriteEncoded(encoded); err != nil {
		return InternalError("cannot encode signed assertion: %v", err)
	}
	if req.Chain {
		account, err := fetchAccountImpl(c, accountKey.AccountID(), user)
		if err != nil {
			return BadRequest("cannot create assertion chain: %v", err)
		}
		if err := enc.Encode(accountKey); err != nil {
			return InternalError("cannot encode assertion chain: %v", err)
		}
		if err := enc.Encode(account); err != nil {
			return InternalError("cannot encode assertion chain: %v", err)
		}
	}
	return SyncResponse(buf.String())
}

// fetchAccountKey returns the account-key assertion for the given key id,
// mirroring the `snap sign` chain lookup (primary key public-key-sha3-384).
func fetchAccountKey(c *Command, keyID string, user *auth.UserState) (*asserts.AccountKey, error) {
	primaryKeys, err := asserts.PrimaryKeyFromHeaders(asserts.AccountKeyType, map[string]string{
		"public-key-sha3-384": keyID,
	})
	if err != nil {
		return nil, fmt.Errorf("cannot query remote assertion: %v", err)
	}
	sto := storeFrom(c.d)
	as, err := sto.Assertion(asserts.AccountKeyType, primaryKeys, user)
	if err != nil {
		return nil, err
	}
	ak, ok := as.(*asserts.AccountKey)
	if !ok {
		return nil, fmt.Errorf("internal error: expected account-key assertion, got %s", as.Type().Name)
	}
	return ak, nil
}

// fetchAccount returns the account assertion for the given account id,
// mirroring the `snap sign` chain lookup (primary key account-id).
func fetchAccount(c *Command, accountID string, user *auth.UserState) (asserts.Assertion, error) {
	primaryKeys, err := asserts.PrimaryKeyFromHeaders(asserts.AccountType, map[string]string{
		"account-id": accountID,
	})
	if err != nil {
		return nil, fmt.Errorf("cannot query remote assertion: %v", err)
	}
	sto := storeFrom(c.d)
	as, err := sto.Assertion(asserts.AccountType, primaryKeys, user)
	if err != nil {
		return nil, err
	}
	return as, nil
}

// listKeys returns public info about the daemon's signing keys (names + key
// ids), equivalent to `snap keys`. No private material is exposed.
func listKeys(_ *Command, r *http.Request, _ *auth.UserState) Response {
	ucred, err := ucrednetGet(r.Context())
	if err != nil {
		return InternalError("cannot identify caller: %v", err)
	}
	if _, err := ucred.InstanceName(); err != nil {
		return InternalError("cannot determine caller snap instance: %v", err)
	}
	kpm, err := signKeypairManager()
	if err != nil {
		return InternalError("cannot open keypair manager: %v", err)
	}
	infos, err := kpm.List()
	if err != nil {
		return InternalError("cannot list keys: %v", err)
	}
	result := make([]map[string]string, 0, len(infos))
	for _, in := range infos {
		result = append(result, map[string]string{"name": in.Name, "sha3-384": in.ID})
	}
	return SyncResponse(result)
}

// assertionTypeSet is the per-connection assertion-type allow-list.
//
// It is always explicit: "all" is never set from connection state. (Resolved
// O4, deny-all: the "assertion-types" plug attribute is mandatory and the
// endpoint refuses to sign any type the connection was not granted. A
// connection without the attribute — e.g. created by an older snapd before the
// attribute became mandatory — cannot sign at all; the interface rejects such
// plugs at install time, so reaching the endpoint with one indicates a policy
// bug and is denied.)
type assertionTypeSet struct {
	all   bool
	types map[string]bool
}

// contains reports whether the statement's assertion type is allowed by the
// set. A missing or non-string "type" header is never allowed.
func (s assertionTypeSet) contains(stmt map[string]any) bool {
	if s.all {
		return true
	}
	typ, _ := stmt["type"].(string)
	return typ != "" && s.types[typ]
}

// connectedPlugAssertionTypes reads the "assertion-types" attribute of the
// caller's active assertion-sign plug.
func connectedPlugAssertionTypes(c *Command, instanceName string) (assertionTypeSet, error) {
	st := c.d.state
	st.Lock()
	defer st.Unlock()
	conns, err := ifacestate.ConnectionStates(st)
	if err != nil {
		return assertionTypeSet{}, fmt.Errorf("cannot get connections: %w", err)
	}
	for refStr, connState := range conns {
		if !connState.Active() || connState.Interface != "assertion-sign" {
			continue
		}
		connRef, err := interfaces.ParseConnRef(refStr)
		if err != nil {
			return assertionTypeSet{}, fmt.Errorf("internal error: invalid connection %q: %w", refStr, err)
		}
		if connRef.PlugRef.Snap.String() != instanceName {
			continue
		}
		types, ok := connState.StaticPlugAttrs["assertion-types"].([]any)
		if !ok || len(types) == 0 {
			// O4 (resolved): omitted or empty "assertion-types" is deny-all.
			return assertionTypeSet{types: map[string]bool{}}, nil
		}
		set := make(map[string]bool, len(types))
		for _, t := range types {
			s, _ := t.(string)
			set[s] = true
		}
		return assertionTypeSet{types: set}, nil
	}
	// No active assertion-sign connection for this instance. The access checker
	// already rejects this path; reaching here indicates a policy bug.
	return assertionTypeSet{}, fmt.Errorf("no active assertion-sign connection for %q", instanceName)
}
