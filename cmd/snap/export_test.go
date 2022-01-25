// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2019 Canonical Ltd
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
	"os"
	"os/user"
	"time"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/cmd/snaplock/runinhibit"
	"github.com/snapcore/snapd/image"
	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/sandbox/selinux"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
)

var RunMain = run

var (
	Client = mkClient

	FirstNonOptionIsRun = firstNonOptionIsRun

	CreateUserDataDirs  = createUserDataDirs
	ResolveApp          = resolveApp
	SnapdHelperPath     = snapdHelperPath
	SortByPath          = sortByPath
	AdviseCommand       = adviseCommand
	Antialias           = antialias
	FormatChannel       = fmtChannel
	PrintDescr          = printDescr
	WrapFlow            = wrapFlow
	TrueishJSON         = trueishJSON
	CompletionHandler   = completionHandler
	MarkForNoCompletion = markForNoCompletion

	CanUnicode           = canUnicode
	ColorTable           = colorTable
	MonoColorTable       = mono
	ColorColorTable      = color
	NoEscColorTable      = noesc
	ColorMixinGetEscapes = (colorMixin).getEscapes
	FillerPublisher      = fillerPublisher
	LongPublisher        = longPublisher
	ShortPublisher       = shortPublisher

	ReadRpc = readRpc

	WriteWarningTimestamp = writeWarningTimestamp
	MaybePresentWarnings  = maybePresentWarnings

	LongSnapDescription     = longSnapDescription
	SnapUsage               = snapUsage
	SnapHelpCategoriesIntro = snapHelpCategoriesIntro
	SnapHelpAllIntro        = snapHelpAllIntro
	SnapHelpAllFooter       = snapHelpAllFooter
	SnapHelpFooter          = snapHelpFooter
	HelpCategories          = helpCategories

	LintArg  = lintArg
	LintDesc = lintDesc

	FixupArg = fixupArg

	InterfacesDeprecationNotice = interfacesDeprecationNotice

	SignalNotify = signalNotify

	SortTimingsTasks = sortTimingsTasks

	PrintInstallHint = printInstallHint

	IsStopping = isStopping

	GetKeypairManager = getKeypairManager
	GenerateKey       = generateKey

	GetSnapDirOptions = getSnapDirOptions
)

func HiddenCmd(descr string, completeHidden bool) *cmdInfo {
	return &cmdInfo{
		shortHelp:      descr,
		hidden:         true,
		completeHidden: completeHidden,
	}
}

type ChangeTimings = changeTimings

func NewInfoWriter(w writeflusher) *infoWriter {
	return &infoWriter{
		writeflusher: w,
		termWidth:    20,
		esc:          &escapes{dash: "--", tick: "*"},
		fmtTime:      func(t time.Time) string { return t.Format(time.Kitchen) },
	}
}

func SetVerbose(iw *infoWriter, verbose bool) {
	iw.verbose = verbose
}

var (
	ClientSnapFromPath          = clientSnapFromPath
	SetupDiskSnap               = (*infoWriter).setupDiskSnap
	SetupSnap                   = (*infoWriter).setupSnap
	MaybePrintServices          = (*infoWriter).maybePrintServices
	MaybePrintCommands          = (*infoWriter).maybePrintCommands
	MaybePrintType              = (*infoWriter).maybePrintType
	PrintSummary                = (*infoWriter).printSummary
	MaybePrintPublisher         = (*infoWriter).maybePrintPublisher
	MaybePrintNotes             = (*infoWriter).maybePrintNotes
	MaybePrintStandaloneVersion = (*infoWriter).maybePrintStandaloneVersion
	MaybePrintBuildDate         = (*infoWriter).maybePrintBuildDate
	MaybePrintContact           = (*infoWriter).maybePrintContact
	MaybePrintBase              = (*infoWriter).maybePrintBase
	MaybePrintPath              = (*infoWriter).maybePrintPath
	MaybePrintSum               = (*infoWriter).maybePrintSum
	MaybePrintCohortKey         = (*infoWriter).maybePrintCohortKey
	MaybePrintHealth            = (*infoWriter).maybePrintHealth
	WaitInhibitUnlock           = waitInhibitUnlock
	WaitWhileInhibited          = waitWhileInhibited
	IsLocked                    = isLocked
)

func MockPollTime(d time.Duration) (restore func()) {
	d0 := pollTime
	pollTime = d
	return func() {
		pollTime = d0
	}
}

func MockMaxGoneTime(d time.Duration) (restore func()) {
	d0 := maxGoneTime
	maxGoneTime = d
	return func() {
		maxGoneTime = d0
	}
}

func MockSyscallExec(f func(string, []string, []string) error) (restore func()) {
	syscallExecOrig := syscallExec
	syscallExec = f
	return func() {
		syscallExec = syscallExecOrig
	}
}

func MockUserCurrent(f func() (*user.User, error)) (restore func()) {
	userCurrentOrig := userCurrent
	userCurrent = f
	return func() {
		userCurrent = userCurrentOrig
	}
}

func MockStoreNew(f func(*store.Config, store.DeviceAndAuthContext) *store.Store) (restore func()) {
	storeNewOrig := storeNew
	storeNew = f
	return func() {
		storeNew = storeNewOrig
	}
}

func MockGetEnv(f func(name string) string) (restore func()) {
	osGetenvOrig := osGetenv
	osGetenv = f
	return func() {
		osGetenv = osGetenvOrig
	}
}

func MockMountInfoPath(newMountInfoPath string) (restore func()) {
	mountInfoPathOrig := mountInfoPath
	mountInfoPath = newMountInfoPath
	return func() {
		mountInfoPath = mountInfoPathOrig
	}
}

func MockOsReadlink(f func(string) (string, error)) (restore func()) {
	osReadlinkOrig := osReadlink
	osReadlink = f
	return func() {
		osReadlink = osReadlinkOrig
	}
}

var AutoImportCandidates = autoImportCandidates

func AliasInfoLess(snapName1, alias1, cmd1, snapName2, alias2, cmd2 string) bool {
	x := aliasInfos{
		&aliasInfo{
			Snap:    snapName1,
			Alias:   alias1,
			Command: cmd1,
		},
		&aliasInfo{
			Snap:    snapName2,
			Alias:   alias2,
			Command: cmd2,
		},
	}
	return x.Less(0, 1)
}

func AssertTypeNameCompletion(match string) []flags.Completion {
	return assertTypeName("").Complete(match)
}

func MockIsStdoutTTY(t bool) (restore func()) {
	oldIsStdoutTTY := isStdoutTTY
	isStdoutTTY = t
	return func() {
		isStdoutTTY = oldIsStdoutTTY
	}
}

func MockIsStdinTTY(t bool) (restore func()) {
	oldIsStdinTTY := isStdinTTY
	isStdinTTY = t
	return func() {
		isStdinTTY = oldIsStdinTTY
	}
}

func MockTimeNow(newTimeNow func() time.Time) (restore func()) {
	oldTimeNow := timeNow
	timeNow = newTimeNow
	return func() {
		timeNow = oldTimeNow
	}
}

func MockTimeutilHuman(h func(time.Time) string) (restore func()) {
	oldH := timeutilHuman
	timeutilHuman = h
	return func() {
		timeutilHuman = oldH
	}
}

func MockWaitConfTimeout(d time.Duration) (restore func()) {
	oldWaitConfTimeout := d
	waitConfTimeout = d
	return func() {
		waitConfTimeout = oldWaitConfTimeout
	}
}

func Wait(cli *client.Client, id string) (*client.Change, error) {
	wmx := waitMixin{}
	wmx.client = cli
	return wmx.wait(id)
}

func ColorMixin(cmode, umode string) colorMixin {
	return colorMixin{
		Color:        cmode,
		unicodeMixin: unicodeMixin{Unicode: umode},
	}
}

func CmdAdviseSnap() *cmdAdviseSnap {
	return &cmdAdviseSnap{}
}

func MockSELinuxIsEnabled(isEnabled func() (bool, error)) (restore func()) {
	old := selinuxIsEnabled
	selinuxIsEnabled = isEnabled
	return func() {
		selinuxIsEnabled = old
	}
}

func MockSELinuxVerifyPathContext(verifypathcon func(string) (bool, error)) (restore func()) {
	old := selinuxVerifyPathContext
	selinuxVerifyPathContext = verifypathcon
	return func() {
		selinuxVerifyPathContext = old
	}
}

func MockSELinuxRestoreContext(restorecon func(string, selinux.RestoreMode) error) (restore func()) {
	old := selinuxRestoreContext
	selinuxRestoreContext = restorecon
	return func() {
		selinuxRestoreContext = old
	}
}

func MockTermSize(newTermSize func() (int, int)) (restore func()) {
	old := termSize
	termSize = newTermSize
	return func() {
		termSize = old
	}
}

func MockImagePrepare(newImagePrepare func(*image.Options) error) (restore func()) {
	old := imagePrepare
	imagePrepare = newImagePrepare
	return func() {
		imagePrepare = old
	}
}

func MockSignalNotify(newSignalNotify func(sig ...os.Signal) (chan os.Signal, func())) (restore func()) {
	old := signalNotify
	signalNotify = newSignalNotify
	return func() {
		signalNotify = old
	}
}

type ServiceName = serviceName

func MockCreateTransientScopeForTracking(fn func(securityTag string, opts *cgroup.TrackingOptions) error) (restore func()) {
	old := cgroupCreateTransientScopeForTracking
	cgroupCreateTransientScopeForTracking = fn
	return func() {
		cgroupCreateTransientScopeForTracking = old
	}
}

func MockConfirmSystemdServiceTracking(fn func(securityTag string) error) (restore func()) {
	old := cgroupConfirmSystemdServiceTracking
	cgroupConfirmSystemdServiceTracking = fn
	return func() {
		cgroupConfirmSystemdServiceTracking = old
	}
}

func MockApparmorSnapAppFromPid(f func(pid int) (string, string, string, error)) (restore func()) {
	old := apparmorSnapAppFromPid
	apparmorSnapAppFromPid = f
	return func() {
		apparmorSnapAppFromPid = old
	}
}

func MockCgroupSnapNameFromPid(f func(pid int) (string, error)) (restore func()) {
	old := cgroupSnapNameFromPid
	cgroupSnapNameFromPid = f
	return func() {
		cgroupSnapNameFromPid = old
	}
}

func MockSyscallUmount(f func(string, int) error) (restore func()) {
	old := syscallUnmount
	syscallUnmount = f
	return func() {
		syscallUnmount = old
	}
}

func MockIoutilTempDir(f func(string, string) (string, error)) (restore func()) {
	old := ioutilTempDir
	ioutilTempDir = f
	return func() {
		ioutilTempDir = old
	}
}

func MockDownloadDirect(f func(snapName string, revision snap.Revision, dlOpts image.DownloadSnapOptions) error) (restore func()) {
	old := downloadDirect
	downloadDirect = f
	return func() {
		downloadDirect = old
	}
}

func MockSnapdAPIInterval(t time.Duration) (restore func()) {
	old := snapdAPIInterval
	snapdAPIInterval = t
	return func() {
		snapdAPIInterval = old
	}
}

func MockSnapdWaitForFullSystemReboot(t time.Duration) (restore func()) {
	old := snapdWaitForFullSystemReboot
	snapdWaitForFullSystemReboot = t
	return func() {
		snapdWaitForFullSystemReboot = old
	}
}

func MockOsChmod(f func(string, os.FileMode) error) (restore func()) {
	old := osChmod
	osChmod = f
	return func() {
		osChmod = old
	}
}

func MockWaitInhibitUnlock(f func(snapName string, waitFor runinhibit.Hint, errCh <-chan error) (bool, error)) (restore func()) {
	old := waitInhibitUnlock
	waitInhibitUnlock = f
	return func() {
		waitInhibitUnlock = old
	}
}

func MockIsLocked(f func(snapName string) (runinhibit.Hint, error)) (restore func()) {
	old := isLocked
	isLocked = f
	return func() {
		isLocked = old
	}
}

func MockIsGraphicalSession(graphical bool) (restore func()) {
	old := isGraphicalSession
	isGraphicalSession = func() bool {
		return graphical
	}
	return func() {
		isGraphicalSession = old
	}
}

func MockAutostartSessionApps(f func(string) error) func() {
	old := autostartSessionApps
	autostartSessionApps = f
	return func() {
		autostartSessionApps = old
	}
}
