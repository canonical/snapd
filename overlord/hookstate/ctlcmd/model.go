// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package ctlcmd

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/client/clientutil"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

var (
	shortModelHelp = i18n.G("Get the active model for this device")
	longModelHelp  = i18n.G(`
The model command returns the active model assertion information for this
device.

By default, the model identification information is presented in a structured,
yaml-like format, but this can be changed to json by using the --json flag.
`)
)

func init() {
	addCommand("model", shortModelHelp, longModelHelp, func() command { return &modelCommand{} })
}

type modelCommand struct {
	baseCommand
	Assertion bool `long:"assertion"`
	Json      bool `long:"json"`
}

type modelCommandFormatter struct {
	snapInfo *snap.Info
}

// GetEscapedDash implements part of the clientutil.ModelFormatter interface
func (mf modelCommandFormatter) GetEscapedDash() string {
	return "--"
}

// LongPublisher implements part of the clientutil.ModelFormatter interface
func (mf modelCommandFormatter) LongPublisher(storeAccountID string) string {
	if mf.snapInfo == nil || mf.snapInfo.Publisher.DisplayName == "" {
		return mf.GetEscapedDash() + "\033[0m"
	}

	storeAccount := mf.snapInfo.Publisher
	badge := ""
	if storeAccount.Validation == "verified" {
		badge = "*"
	}

	// NOTE this makes e.g. 'Potato' == 'potato', and 'Potato Team' == 'potato-team',
	// but 'Potato Team' != 'potatoteam', 'Potato Inc.' != 'potato' (in fact 'Potato Inc.' != 'potato-inc')
	if strings.EqualFold(strings.Replace(storeAccount.Username, "-", " ", -1), storeAccount.DisplayName) {
		return storeAccount.DisplayName + badge + "\033[0m"
	}
	return fmt.Sprintf("%s (%s%s%s)", storeAccount.DisplayName, storeAccount.Username, badge, "\033[0m")
}

func newTabWriter(output io.Writer) *tabwriter.Writer {
	minWidth := 2
	tabWidth := 2
	padding := 2
	padchar := byte(' ')
	return tabwriter.NewWriter(output, minWidth, tabWidth, padding, padchar, 0)
}

// reportError prints the error message to stderr
func (c *modelCommand) reportError(format string, a ...interface{}) {
	w := newTabWriter(c.stderr)
	fmt.Fprintf(w, format, a...)
	w.Flush()
}

// hasSnapdControl returns true if the requesting snap has the snapd-control plug
// and only if it is connected as well.
var hasSnapdControlInterface = func(st *state.State, snapName string) bool {
	conns, err := ifacestate.ConnectionStates(st)
	if err != nil {
		return false
	}
	for refStr, connState := range conns {
		if connState.Undesired || connState.Interface != "snapd-control" {
			continue
		}
		connRef, err := interfaces.ParseConnRef(refStr)
		if err != nil {
			return false
		}
		if connRef.PlugRef.Snap == snapName {
			return true
		}
	}
	return false
}

// getSnapInfo is a helper utility to read the snap.Info for the requesting snap
// which also fills the publisher information.
func getSnapInfo(st *state.State, snapName string) (*snap.Info, error) {
	var snapst snapstate.SnapState
	if err := snapstate.Get(st, snapName, &snapst); err != nil {
		return nil, fmt.Errorf("failed to get snapstate for snap %s: %v", snapName, err)
	}

	snapInfo, err := snapst.CurrentInfo()
	if err != nil {
		return nil, err
	}

	snapInfo.Publisher, err = assertstate.PublisherStoreAccount(st, snapInfo.SnapID)
	return snapInfo, err
}

// checkGadgetOrModel verifies that the current snap context is either a gadget snap or that
// the snap shares the same publisher as the current model assertion.
func (c *modelCommand) checkGadgetOrModel(st *state.State, deviceCtx snapstate.DeviceContext, snapInfo *snap.Info) error {
	// We allow the usage of this command if one of the following is true
	// 1. The requesting snap must be a gadget
	// 2. Come from the same brand as the device model assertion
	// 3. Have the snapd-control plug
	if snapType := snapInfo.Type(); snapType == snap.TypeGadget {
		return nil
	}
	if snapInfo.Publisher.ID == deviceCtx.Model().BrandID() {
		return nil
	}
	if hasSnapdControlInterface(st, snapInfo.SnapName()) {
		return nil
	}

	c.reportError("cannot get model assertion for snap %q: not a gadget or from the same brand as the device model assertion\n",
		snapInfo.SnapName())
	return fmt.Errorf("insufficient permissions to get model assertion for snap %q", snapInfo.SnapName())
}

func findSerialAssertion(st *state.State, modelAssertion *asserts.Model) *asserts.Serial {
	assertions, err := assertstate.DB(st).FindMany(asserts.SerialType, map[string]string{
		"brand-id": modelAssertion.BrandID(),
		"model":    modelAssertion.Model(),
	})
	if err != nil || len(assertions) == 0 {
		return nil
	}

	sort.Slice(assertions, func(i, j int) bool {
		// get timestamps from the assertion
		iTimeString := assertions[i].HeaderString("timestamp")
		jTimeString := assertions[j].HeaderString("timestamp")

		t1, err := time.Parse(time.RFC3339, iTimeString)
		if err != nil {
			return false
		}
		t2, err := time.Parse(time.RFC3339, jTimeString)
		if err != nil {
			return false
		}
		return t1.Before(t2)
	})
	serial := assertions[0].(*asserts.Serial)
	return serial
}

func (c *modelCommand) Execute([]string) error {
	context, err := c.ensureContext()
	if err != nil {
		return err
	}

	// ignore the valid bool as we just pass the task whether it is
	// nil or not.
	task, _ := context.Task()
	st := context.State()
	st.Lock()
	defer st.Unlock()

	deviceCtx, err := snapstate.DeviceCtx(st, task, nil)
	if err != nil {
		return err
	}

	// We only return an error in case we could not the get the snap.Info
	// structure, and 'ignore' any error that caused us not to get the store
	// account publisher
	snapInfo, err := getSnapInfo(st, context.InstanceName())
	if snapInfo == nil {
		return err
	}

	if err := c.checkGadgetOrModel(st, deviceCtx, snapInfo); err != nil {
		return err
	}

	// use the same tab-writer settings as the 'snap model' in cmd_list.go
	w := newTabWriter(c.stdout)
	defer w.Flush()

	opts := clientutil.PrintModelAssertionOptions{
		// We cannot use terminal.GetSize() here as this is executed by snapd.
		// So we choose 80 as the default terminal width.
		TermWidth: 80,
		// Request absolute time format, otherwise it will be formatted as human
		// readable strings.
		AbsTime: true,
		// According to the spec we always assume verbose mode when using the
		// model command through snapctl.
		Verbose:   true,
		Assertion: c.Assertion,
	}

	serialAssertion := findSerialAssertion(st, deviceCtx.Model())
	if c.Json {
		if err := clientutil.PrintModelAssertionJSON(w, *deviceCtx.Model(), serialAssertion, opts); err != nil {
			return err
		}
	} else {
		modelFormatter := modelCommandFormatter{
			snapInfo: snapInfo,
		}
		if err := clientutil.PrintModelAssertion(w, *deviceCtx.Model(), serialAssertion, modelFormatter, opts); err != nil {
			return err
		}
	}
	return nil
}
