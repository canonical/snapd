// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/signtool"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
)

type cmdClusterCommit struct {
	clientMixin
}

var shortClusterCommitHelp = i18n.G("Commit a signed cluster assertion")
var longClusterCommitHelp = i18n.G(`
The cluster commit command retrieves the uncommitted cluster state,
signs it with the specified key, and commits the signed assertion.

This command should be run after cluster assembly has completed successfully.
The signing key must be specified via the CLUSTER_SIGN_KEY environment variable.
The key must be available in your GPG keyring or external key manager,
and its corresponding account-key assertion must already be acked in the system.

Example:
  CLUSTER_SIGN_KEY=my-signing-key snap cluster commit
`)

func init() {
	addClusterCommand("commit", shortClusterCommitHelp, longClusterCommitHelp, func() flags.Commander {
		return &cmdClusterCommit{}
	}, nil, nil)
}

func (x *cmdClusterCommit) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	// get the key name from environment variable
	keyName := os.Getenv("CLUSTER_SIGN_KEY")
	if keyName == "" {
		return fmt.Errorf(i18n.G("CLUSTER_SIGN_KEY environment variable must be set"))
	}

	// get uncommitted cluster state
	state, err := x.client.GetClusterUncommittedState()
	if err != nil {
		return fmt.Errorf("cannot get uncommitted cluster state: %v", err)
	}

	// convert state to headers for signing
	headers := convertStateToHeaders(state)

	// get the keypair manager
	keypairMgr, err := signtool.GetKeypairManager()
	if err != nil {
		return err
	}

	// get the private key
	privKey, err := keypairMgr.GetByName(keyName)
	if err != nil {
		// TRANSLATORS: %q is the key name, %v the error message
		return fmt.Errorf(i18n.G("cannot use %q key: %v"), keyName, err)
	}

	// get account-key assertion if we need to build a chain
	as, err := mustGetOneAssert("account-key", map[string]string{"public-key-sha3-384": privKey.PublicKey().ID()})
	if err != nil {
		return fmt.Errorf(i18n.G("cannot create assertion chain: %w"), err)
	}

	ak := as.(*asserts.AccountKey)
	headers["authority-id"] = ak.AccountID()

	// convert headers to JSON for signing
	statement, err := json.Marshal(headers)
	if err != nil {
		return fmt.Errorf("cannot marshal headers: %v", err)
	}

	// sign the assertion
	signOpts := signtool.Options{
		KeyID:      privKey.PublicKey().ID(),
		AccountKey: ak,
		Statement:  statement,
	}

	encoded, err := signtool.Sign(&signOpts, keypairMgr)
	if err != nil {
		return fmt.Errorf("cannot sign cluster assertion: %v", err)
	}

	// decode the signed assertion to get the cluster ID
	decoded, err := asserts.Decode(encoded)
	if err != nil {
		return fmt.Errorf("cannot decode signed assertion: %v", err)
	}

	cluster, ok := decoded.(*asserts.Cluster)
	if !ok {
		return fmt.Errorf("internal error: signed assertion is not a cluster assertion")
	}

	// build the assertion stream to submit
	buf := bytes.NewBuffer(nil)
	enc := asserts.NewEncoder(buf)

	// add the cluster assertion
	if err := enc.WriteEncoded(encoded); err != nil {
		return fmt.Errorf("cannot encode cluster assertion: %v", err)
	}

	// add account-key assertion
	if err := enc.Encode(ak); err != nil {
		return fmt.Errorf("cannot encode account-key assertion: %v", err)
	}

	// add account assertion
	account, err := mustGetOneAssert("account", map[string]string{"account-id": ak.AccountID()})
	if err != nil {
		return fmt.Errorf(i18n.G("cannot create assertion chain: %w"), err)
	}
	if err := enc.Encode(account); err != nil {
		return fmt.Errorf("cannot encode account assertion: %v", err)
	}

	// submit the assertion(s) to the system
	if err := x.client.Ack(buf.Bytes()); err != nil {
		return fmt.Errorf("cannot submit cluster assertion: %v", err)
	}

	// commit the cluster state by ID (using the new /v2/cluster/commit endpoint)
	if err := x.client.CommitClusterAssertion(cluster.ClusterID()); err != nil {
		return fmt.Errorf("cannot commit cluster state: %v", err)
	}

	fmt.Fprintf(Stdout, i18n.G("Cluster assertion committed successfully.\n"))
	return nil
}

// convertStateToHeaders converts UncommittedClusterState to assertion headers
func convertStateToHeaders(state client.UncommittedClusterState) map[string]any {
	var devices []any
	if len(state.Devices) > 0 {
		devices = make([]any, 0, len(state.Devices))
		for _, d := range state.Devices {
			addresses := make([]any, 0, len(d.Addresses))
			for _, addr := range d.Addresses {
				addresses = append(addresses, addr)
			}

			devices = append(devices, map[string]any{
				"id":        strconv.Itoa(d.ID),
				"brand-id":  d.BrandID,
				"model":     d.Model,
				"serial":    d.Serial,
				"addresses": addresses,
			})
		}
	}

	var subclusters []any
	if len(state.Subclusters) > 0 {
		subclusters = make([]any, 0, len(state.Subclusters))
		for _, sc := range state.Subclusters {
			ids := make([]any, 0, len(sc.Devices))
			for _, id := range sc.Devices {
				ids = append(ids, strconv.Itoa(id))
			}

			subcluster := map[string]any{
				"name":    sc.Name,
				"devices": ids,
			}

			if len(sc.Snaps) > 0 {
				snaps := make([]any, 0, len(sc.Snaps))
				for _, snap := range sc.Snaps {
					snaps = append(snaps, map[string]any{
						"state":    snap.State,
						"instance": snap.Instance,
						"channel":  snap.Channel,
					})
				}
				subcluster["snaps"] = snaps
			}

			subclusters = append(subclusters, subcluster)
		}
	}

	return map[string]any{
		"type":        "cluster",
		"cluster-id":  state.ClusterID,
		"sequence":    strconv.Itoa(state.Sequence + 1), // TODO: handle sequences properly
		"devices":     devices,
		"subclusters": subclusters,
		"timestamp":   state.CompletedAt.Format(time.RFC3339),
	}
}
