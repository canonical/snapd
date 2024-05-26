// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2020 Canonical Ltd
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

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/client/clientutil"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/healthstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

var errNoSnap = errors.New("snap not installed")

type aboutSnap struct {
	info           *snap.Info
	snapst         *snapstate.SnapState
	health         *client.SnapHealth
	refreshInhibit *client.SnapRefreshInhibit

	hold       time.Time
	gatingHold time.Time
}

// localSnapInfo returns the information about the current snap for the given
// name plus the SnapState with the active flag and other snap revisions.
func localSnapInfo(st *state.State, name string) (aboutSnap, error) {
	st.Lock()
	defer st.Unlock()

	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(st, name, &snapst))
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return aboutSnap{}, fmt.Errorf("cannot consult state: %v", err)
	}

	info := mylog.Check2(snapst.CurrentInfo())
	if err == snapstate.ErrNoCurrent {
		return aboutSnap{}, errNoSnap
	}

	info.Publisher = mylog.Check2(assertstate.PublisherStoreAccount(st, info.SnapID))

	health := mylog.Check2(healthstate.Get(st, name))

	userHold, gatingHold := mylog.Check3(getUserAndGatingHolds(st, name))

	refreshInhibit := clientSnapRefreshInhibit(st, &snapst, name)

	return aboutSnap{
		info:           info,
		snapst:         &snapst,
		health:         clientHealthFromHealthstate(health),
		refreshInhibit: refreshInhibit,
		hold:           userHold,
		gatingHold:     gatingHold,
	}, nil
}

func getUserAndGatingHolds(st *state.State, name string) (userHold, gatingHold time.Time, err error) {
	userHold = mylog.Check2(snapstateSystemHold(st, name))

	gatingHold = mylog.Check2(snapstateLongestGatingHold(st, name))

	return userHold, gatingHold, err
}

type snapSelect int

const (
	snapSelectNone snapSelect = iota
	snapSelectAll
	snapSelectEnabled
	snapSelectRefreshInhibited
)

// allLocalSnapInfos returns the information about the all current snaps and their SnapStates.
func allLocalSnapInfos(st *state.State, sel snapSelect, wanted map[string]bool) ([]aboutSnap, error) {
	st.Lock()
	defer st.Unlock()

	snapStates := mylog.Check2(snapstate.All(st))

	about := make([]aboutSnap, 0, len(snapStates))

	healths := mylog.Check2(healthstate.All(st))

	for name, snapst := range snapStates {
		if len(wanted) > 0 && !wanted[name] {
			continue
		}
		health := clientHealthFromHealthstate(healths[name])

		userHold, gatingHold := mylog.Check3(getUserAndGatingHolds(st, name))

		refreshInhibit := clientSnapRefreshInhibit(st, snapst, name)
		if sel == snapSelectRefreshInhibited && refreshInhibit == nil {
			// skip snaps whose refresh is not inhibited
			continue
		}

		var aboutThis []aboutSnap
		var info *snap.Info
		if sel == snapSelectAll {
			for _, si := range snapst.Sequence.SideInfos() {
				info = mylog.Check2(snap.ReadInfo(name, si))

				// single revision may be broken

				// clear the error

				info.Publisher = mylog.Check2(assertstate.PublisherStoreAccount(st, si.SnapID))

				abSnap := aboutSnap{
					info:           info,
					snapst:         snapst,
					health:         health,
					refreshInhibit: refreshInhibit,
					hold:           userHold,
					gatingHold:     gatingHold,
				}
				aboutThis = append(aboutThis, abSnap)
			}
		} else {
			info = mylog.Check2(snapst.CurrentInfo())

			info.Publisher = mylog.Check2(assertstate.PublisherStoreAccount(st, info.SnapID))

			abSnap := aboutSnap{
				info:           info,
				snapst:         snapst,
				health:         health,
				refreshInhibit: refreshInhibit,
				hold:           userHold,
				gatingHold:     gatingHold,
			}
			aboutThis = append(aboutThis, abSnap)
		}
		about = append(about, aboutThis...)
	}

	return about, nil
}

func clientHealthFromHealthstate(h *healthstate.HealthState) *client.SnapHealth {
	if h == nil {
		return nil
	}
	return &client.SnapHealth{
		Revision:  h.Revision,
		Timestamp: h.Timestamp,
		Status:    h.Status.String(),
		Message:   h.Message,
		Code:      h.Code,
	}
}

func clientSnapRefreshInhibit(st *state.State, snapst *snapstate.SnapState, instanceName string) *client.SnapRefreshInhibit {
	proceedTime := snapst.RefreshInhibitProceedTime(st)
	if proceedTime.After(time.Now()) || snapstate.IsSnapMonitored(st, instanceName) {
		return &client.SnapRefreshInhibit{
			ProceedTime: proceedTime,
		}
	}

	return nil
}

func mapLocal(about aboutSnap, sd clientutil.StatusDecorator) *client.Snap {
	localSnap, snapst := about.info, about.snapst
	result := mylog.Check2(clientutil.ClientSnapFromSnapInfo(localSnap, sd))

	result.InstalledSize = localSnap.Size

	if icon := snapIcon(localSnap); icon != "" {
		result.Icon = icon
	}

	result.Status = "installed"
	if snapst.Active && localSnap.Revision == snapst.Current {
		result.Status = "active"
	}

	result.TrackingChannel = snapst.TrackingChannel
	result.IgnoreValidation = snapst.IgnoreValidation
	result.CohortKey = snapst.CohortKey
	result.DevMode = snapst.DevMode
	result.TryMode = snapst.TryMode
	result.JailMode = snapst.JailMode
	result.MountedFrom = localSnap.MountFile()
	if result.TryMode {
		// Readlink instead of EvalSymlinks because it's only expected
		// to be one level, and should still resolve if the target does
		// not exist (this might help e.g. snapcraft clean up after a
		// prime dir)
		result.MountedFrom, _ = os.Readlink(result.MountedFrom)
	}
	result.Health = about.health
	result.RefreshInhibit = about.refreshInhibit

	if !about.hold.IsZero() {
		result.Hold = &about.hold
	}
	if !about.gatingHold.IsZero() {
		result.GatingHold = &about.gatingHold
	}
	return result
}

// snapIcon tries to find the icon inside the snap
func snapIcon(info snap.PlaceInfo) string {
	found, _ := filepath.Glob(filepath.Join(info.MountDir(), "meta", "gui", "icon.*"))
	if len(found) == 0 {
		return ""
	}

	return found[0]
}
