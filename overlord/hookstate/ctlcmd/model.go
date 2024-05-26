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
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ddkwork/golibrary/mylog"
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
// essentially this functions reimplements the same logic as cmd/snap/color.go
// but without all the fancy formatting as we don't need it. We also will not use
// the unicode characters as the output is formatted for API rather than human
// consumption
func (mf modelCommandFormatter) LongPublisher(storeAccountID string) string {
	if mf.snapInfo == nil || mf.snapInfo.Publisher.DisplayName == "" {
		return mf.GetEscapedDash()
	}

	storeAccount := mf.snapInfo.Publisher
	var badge string
	switch storeAccount.Validation {
	case "verified":
		badge = "**"
	case "starred":
		badge = "*"
	}

	// NOTE this makes e.g. 'Potato' == 'potato', and 'Potato Team' == 'potato-team',
	// but 'Potato Team' != 'potatoteam', 'Potato Inc.' != 'potato' (in fact 'Potato Inc.' != 'potato-inc')
	if strings.EqualFold(strings.Replace(storeAccount.Username, "-", " ", -1), storeAccount.DisplayName) {
		return storeAccount.DisplayName + badge
	}
	return fmt.Sprintf("%s (%s%s)", storeAccount.DisplayName, storeAccount.Username, badge)
}

func (c *modelCommand) newTabWriter(output io.Writer) *tabwriter.Writer {
	minWidth := 2
	tabWidth := 2
	padding := 2
	padchar := byte(' ')
	return tabwriter.NewWriter(output, minWidth, tabWidth, padding, padchar, 0)
}

// reportError prints the error message to stderr
func (c *modelCommand) reportError(format string, a ...interface{}) {
	w := c.newTabWriter(c.stderr)
	fmt.Fprintf(w, format, a...)
	w.Flush()
}

// hasSnapdControlInterface returns true if the requesting snap has the
// snapd-control plug and only if it is connected as well.
func (c *modelCommand) hasSnapdControlInterface(st *state.State, snapName string) (bool, error) {
	conns := mylog.Check2(ifacestate.ConnectionStates(st))

	for refStr, connState := range conns {
		if connState.Undesired || connState.Interface != "snapd-control" {
			continue
		}
		connRef := mylog.Check2(interfaces.ParseConnRef(refStr))

		if connRef.PlugRef.Snap == snapName {
			return true, nil
		}
	}
	return false, nil
}

// getSnapInfoWithPublisher is a helper utility to read the snap.Info for the requesting snap
// which also fills the publisher information.
func (c *modelCommand) getSnapInfoWithPublisher(st *state.State, snapName string) (*snap.Info, error) {
	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(st, snapName, &snapst))

	snapInfo := mylog.Check2(snapst.CurrentInfo())

	snapInfo.Publisher = mylog.Check2(assertstate.PublisherStoreAccount(st, snapInfo.SnapID))
	return snapInfo, err
}

// checkPermissions verifies that the snap described by snapInfo is allowed to
// read the model assertion of deviceCtx.
// We allow the usage of this command if one of the following is true
// 1. The requesting snap must be a gadget
// 2. Come from the same brand as the device model assertion
// 3. Have the snapd-control plug
func (c *modelCommand) checkPermissions(st *state.State, deviceCtx snapstate.DeviceContext, snapInfo *snap.Info) error {
	if snapType := snapInfo.Type(); snapType == snap.TypeGadget {
		return nil
	}
	if snapInfo.Publisher.ID == deviceCtx.Model().BrandID() {
		return nil
	}
	if conn := mylog.Check2(c.hasSnapdControlInterface(st, snapInfo.SnapName())); err != nil {
		return fmt.Errorf("cannot check for snapd-control interface: %v", err)
	} else if conn {
		return nil
	}

	c.reportError("cannot get model assertion for snap %q: "+
		"must be either a gadget snap, from the same publisher as the model "+
		"or have the snapd-control interface\n", snapInfo.SnapName())
	return fmt.Errorf("insufficient permissions to get model assertion for snap %q", snapInfo.SnapName())
}

// findSerialAssertion is a helper function to find the newest matching serial assertion
// for the provided model assertion.
func (c *modelCommand) findSerialAssertion(st *state.State, modelAssertion *asserts.Model) (*asserts.Serial, error) {
	assertions := mylog.Check2(assertstate.DB(st).FindMany(asserts.SerialType, map[string]string{
		"brand-id": modelAssertion.BrandID(),
		"model":    modelAssertion.Model(),
	}))
	if err != nil || len(assertions) == 0 {
		return nil, err
	}

	// Helper to parse the timestamp embedded in the assertion. There
	// is a Timestamp method for the serial assertion, so we cast it
	// and use that
	getAssertionTime := func(a asserts.Assertion) time.Time {
		serial := a.(*asserts.Serial)
		return serial.Timestamp()
	}

	sort.Slice(assertions, func(i, j int) bool {
		t1 := getAssertionTime(assertions[i])
		t2 := getAssertionTime(assertions[j])
		// sort in descending order to get the newest first
		return t2.Before(t1)
	})
	serial := assertions[0].(*asserts.Serial)
	return serial, nil
}

func (c *modelCommand) Execute([]string) error {
	context := mylog.Check2(c.ensureContext())

	st := context.State()
	st.Lock()
	defer st.Unlock()

	// ignore the valid bool as we just pass the task whether it is
	// nil or not.
	task, _ := context.Task()
	deviceCtx := mylog.Check2(snapstate.DeviceCtx(st, task, nil))

	// We only return an error in case we could not the get the snap.Info
	// structure, and 'ignore' any error that caused us not to get the store
	// account publisher
	snapInfo := mylog.Check2(c.getSnapInfoWithPublisher(st, context.InstanceName()))
	if snapInfo == nil {
		return err
	}
	mylog.Check(c.checkPermissions(st, deviceCtx, snapInfo))

	// use the same tab-writer settings as the 'snap model' in cmd_list.go
	w := c.newTabWriter(c.stdout)
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

	serialAssertion := mylog.Check2(c.findSerialAssertion(st, deviceCtx.Model()))
	// Ignore the error in case the serial assertion wasn't found. We will
	// then use the model assertion instead.
	if err != nil && !errors.Is(err, &asserts.NotFoundError{}) {
		return err
	}

	if c.Json {
		mylog.Check(clientutil.PrintModelAssertionJSON(w, *deviceCtx.Model(), serialAssertion, opts))
	} else {
		modelFormatter := modelCommandFormatter{
			snapInfo: snapInfo,
		}
		mylog.Check(clientutil.PrintModelAssertion(w, *deviceCtx.Model(), serialAssertion, modelFormatter, opts))

	}
	return nil
}
