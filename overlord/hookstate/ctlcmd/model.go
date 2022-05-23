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
	"strings"
	"text/tabwriter"

	"github.com/snapcore/snapd/client/clientutil"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
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

func interfaceConnected(st *state.State, snapName, ifName string) bool {
	conns, err := ifacerepo.Get(st).Connected(snapName, ifName)
	return err == nil && len(conns) > 0
}

// hasSnapdControl returns true if the requesting snap has the snapd-control plug
// and only if it is connected as well.
func hasSnapdControl(st *state.State, snapInfo *snap.Info) bool {
	// Always get the current info even if the snap is currently
	// being operated on or if its disabled.
	if snapInfo.Broken != "" {
		return false
	}

	// The snap must have a snap declaration (implies that
	// its from the store)
	if _, err := assertstate.SnapDeclaration(st, snapInfo.SideInfo.SnapID); err != nil {
		return false
	}

	for _, plugInfo := range snapInfo.Plugs {
		if plugInfo.Interface == "snapd-control" {
			snapName := snapInfo.InstanceName()
			plugName := plugInfo.Name
			if interfaceConnected(st, snapName, plugName) {
				return true
			}
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

	snapInfo.Publisher, err = assertstate.PublisherAccount(st, snapInfo.SnapID)
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
	if hasSnapdControl(st, snapInfo) {
		return nil
	}

	c.reportError("cannot get model assertion for snap %q: not a gadget or from the same brand as the device model assertion\n",
		snapInfo.SnapName())
	return fmt.Errorf("insufficient permissions to get model assertion for snap %q", snapInfo.SnapName())
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

	if c.Json {
		if err := clientutil.PrintModelAssertionJSON(w, *deviceCtx.Model(), nil, opts); err != nil {
			return err
		}
	} else {
		modelFormatter := modelCommandFormatter{
			snapInfo: snapInfo,
		}
		if err := clientutil.PrintModelAssertion(w, *deviceCtx.Model(), nil, modelFormatter, opts); err != nil {
			return err
		}
	}
	return nil
}
