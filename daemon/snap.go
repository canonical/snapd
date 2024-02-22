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
	info   *snap.Info
	snapst *snapstate.SnapState
	health *client.SnapHealth

	hold                      time.Time
	gatingHold                time.Time
	refreshInhibitProceedTime time.Time
}

// localSnapInfo returns the information about the current snap for the given
// name plus the SnapState with the active flag and other snap revisions.
func localSnapInfo(st *state.State, name string) (aboutSnap, error) {
	st.Lock()
	defer st.Unlock()

	var snapst snapstate.SnapState
	err := snapstate.Get(st, name, &snapst)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return aboutSnap{}, fmt.Errorf("cannot consult state: %v", err)
	}

	info, err := snapst.CurrentInfo()
	if err == snapstate.ErrNoCurrent {
		return aboutSnap{}, errNoSnap
	}
	if err != nil {
		return aboutSnap{}, fmt.Errorf("cannot read snap details: %v", err)
	}

	info.Publisher, err = assertstate.PublisherStoreAccount(st, info.SnapID)
	if err != nil {
		return aboutSnap{}, err
	}

	health, err := healthstate.Get(st, name)
	if err != nil {
		return aboutSnap{}, err
	}

	userHold, gatingHold, err := getUserAndGatingHolds(st, name)
	if err != nil {
		return aboutSnap{}, InternalError("%v", err)
	}
	refreshInhibitProceedTime := snapst.RefreshInhibitProceedTime(st)

	return aboutSnap{
		info:                      info,
		snapst:                    &snapst,
		health:                    clientHealthFromHealthstate(health),
		hold:                      userHold,
		gatingHold:                gatingHold,
		refreshInhibitProceedTime: refreshInhibitProceedTime,
	}, nil
}

func getUserAndGatingHolds(st *state.State, name string) (userHold, gatingHold time.Time, err error) {
	userHold, err = snapstateSystemHold(st, name)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}

	gatingHold, err = snapstateLongestGatingHold(st, name)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}

	return userHold, gatingHold, err
}

// allLocalSnapInfos returns the information about the all current snaps and their SnapStates.
func allLocalSnapInfos(st *state.State, all bool, wanted map[string]bool) ([]aboutSnap, error) {
	st.Lock()
	defer st.Unlock()

	snapStates, err := snapstate.All(st)
	if err != nil {
		return nil, err
	}
	about := make([]aboutSnap, 0, len(snapStates))

	healths, err := healthstate.All(st)
	if err != nil {
		return nil, err
	}

	for name, snapst := range snapStates {
		if len(wanted) > 0 && !wanted[name] {
			continue
		}
		health := clientHealthFromHealthstate(healths[name])

		userHold, gatingHold, err := getUserAndGatingHolds(st, name)
		if err != nil {
			return nil, err
		}
		refreshInhibitProceedTime := snapst.RefreshInhibitProceedTime(st)

		var aboutThis []aboutSnap
		var info *snap.Info
		if all {
			for _, si := range snapst.Sequence.SideInfos() {
				info, err = snap.ReadInfo(name, si)
				if err != nil {
					// single revision may be broken
					_, instanceKey := snap.SplitInstanceName(name)
					info = &snap.Info{
						SideInfo:    *si,
						InstanceKey: instanceKey,
						Broken:      err.Error(),
					}
					// clear the error
					err = nil
				}
				info.Publisher, err = assertstate.PublisherStoreAccount(st, si.SnapID)
				if err != nil {
					return nil, err
				}
				abSnap := aboutSnap{
					info:                      info,
					snapst:                    snapst,
					health:                    health,
					hold:                      userHold,
					gatingHold:                gatingHold,
					refreshInhibitProceedTime: refreshInhibitProceedTime,
				}
				aboutThis = append(aboutThis, abSnap)
			}
		} else {
			info, err = snapst.CurrentInfo()
			if err != nil {
				return nil, err
			}

			info.Publisher, err = assertstate.PublisherStoreAccount(st, info.SnapID)
			if err != nil {
				return nil, err
			}

			abSnap := aboutSnap{
				info:                      info,
				snapst:                    snapst,
				health:                    health,
				hold:                      userHold,
				gatingHold:                gatingHold,
				refreshInhibitProceedTime: refreshInhibitProceedTime,
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

func mapLocal(about aboutSnap, sd clientutil.StatusDecorator) *client.Snap {
	localSnap, snapst := about.info, about.snapst
	result, err := clientutil.ClientSnapFromSnapInfo(localSnap, sd)
	if err != nil {
		logger.Noticef("cannot get full app info for snap %q: %v", localSnap.InstanceName(), err)
	}
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

	if !about.hold.IsZero() {
		result.Hold = &about.hold
	}
	if !about.gatingHold.IsZero() {
		result.GatingHold = &about.gatingHold
	}
	if !about.refreshInhibitProceedTime.IsZero() {
		result.RefreshInhibitProceedTime = &about.refreshInhibitProceedTime
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
