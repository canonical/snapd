// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"strings"
	"text/tabwriter"

	"github.com/snapcore/snapd/client/clientutil"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/devicestate"
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

Similarly, the active serial assertion can be used for the output instead of the
model assertion.
`)
)

func init() {
	addCommand("model", shortModelHelp, longModelHelp, func() command { return &modelCommand{} })
}

type modelCommand struct {
	baseCommand
	Assertion bool `long:"assertion"`
	Json      bool `long:"json"`
	TermWidth int  `long:"term-width"`
}

type modelCommandFormatter struct {
	storeAccount *snap.StoreAccount
}

func (mf modelCommandFormatter) GetEscapedDash() string {
	return "--"
}

func (mf modelCommandFormatter) GetEscapedTick() string {
	return "*"
}

func (mf modelCommandFormatter) GetPublisher() string {
	if mf.storeAccount == nil {
		return mf.GetEscapedDash() + "\033[0m"
	}

	badge := ""
	if mf.storeAccount.Validation == "verified" {
		badge = mf.GetEscapedTick()
	}

	// NOTE this makes e.g. 'Potato' == 'potato', and 'Potato Team' == 'potato-team',
	// but 'Potato Team' != 'potatoteam', 'Potato Inc.' != 'potato' (in fact 'Potato Inc.' != 'potato-inc')
	if strings.EqualFold(strings.Replace(mf.storeAccount.Username, "-", " ", -1), mf.storeAccount.DisplayName) {
		return mf.storeAccount.DisplayName + badge + "\033[0m"
	}
	return fmt.Sprintf("%s (%s%s%s)", mf.storeAccount.DisplayName, mf.storeAccount.Username, badge, "\033[0m")
}

func (c *modelCommand) reportError(format string, a ...interface{}) {
	w := tabwriter.NewWriter(c.stderr, 2, 2, 2, ' ', 0)
	fmt.Fprintf(w, format, a...)
}

func (c *modelCommand) checkGadgetOrModel(st *state.State, snapName string) error {
	st.Lock()
	defer st.Unlock()

	var snapst snapstate.SnapState
	if err := snapstate.Get(st, snapName, &snapst); err != nil {
		return fmt.Errorf("failed to get snapstate for snap %s: %v", snapName, err)
	}

	// get the brand of the current snap
	snapInfo, err := snapst.CurrentInfo()
	if err != nil {
		return err
	}

	deviceCtx, err := devicestate.DeviceCtx(st, nil, nil)
	if err != nil {
		return err
	}

	// the request snap must be a gadget or come from the same
	// brand as the device model assertion
	if snapType := snapInfo.Type(); snapType != snap.TypeGadget {
		if snapInfo.Publisher.ID != deviceCtx.Model().BrandID() {
			c.reportError("cannot get model assertion for snap %q: not a gadget or from the same brand as the device model assertion\n", snapName)
			return fmt.Errorf("insufficient permissions to get model assertion for snap %q", snapName)
		}
	}
	return nil
}

func (c *modelCommand) Execute([]string) error {
	context, err := c.ensureContext()
	if err != nil {
		return err
	}

	st := context.State()
	if err := c.checkGadgetOrModel(st, context.InstanceName()); err != nil {
		return err
	}

	st.Lock()
	deviceCtx, err := snapstate.DeviceCtx(st, nil, nil)
	if err != nil {
		return err
	}
	st.Unlock()

	format := clientutil.MODELWRITER_YAML_FORMAT
	if c.Json {
		format = clientutil.MODELWRITER_JSON_FORMAT
	}

	var modelFormatter modelCommandFormatter
	modelAssertation := deviceCtx.Model()
	w := tabwriter.NewWriter(c.stdout, 2, 2, 2, ' ', 0)
	if err = clientutil.PrintModelAssertation(w, format, modelFormatter, c.TermWidth, false, false, true, c.Assertion, modelAssertation, nil); err != nil {
		return err
	}
	w.Flush()
	return nil
}
