// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2021 Canonical Ltd
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

package devicestate

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/kernel/fde"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/devicestate/internal"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/overlord/storecontext"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/sysconfig"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/timeutil"
	"github.com/snapcore/snapd/timings"
)

var (
	cloudInitStatus   = sysconfig.CloudInitStatus
	restrictCloudInit = sysconfig.RestrictCloudInit
)

// EarlyConfig is a hook set by configstate that can process early configuration
// during managers' startup.
var EarlyConfig func(st *state.State, preloadGadget func() (sysconfig.Device, *gadget.Info, error)) error

// DeviceManager is responsible for managing the device identity and device
// policies.
type DeviceManager struct {
	// sysMode is the system mode from modeenv or "" on pre-UC20,
	// use SystemMode instead
	sysMode string
	// saveAvailable keeps track whether /var/lib/snapd/save
	// is available, i.e. exists and is mounted from ubuntu-save
	// if the latter exists.
	// TODO: evolve this to state to track things if we start mounting
	// save as rw vs ro, or mount/umount it fully on demand
	saveAvailable bool

	state   *state.State
	hookMgr *hookstate.HookManager

	cachedKeypairMgr asserts.KeypairManager

	// newStore can make new stores for remodeling
	newStore func(storecontext.DeviceBackend) snapstate.StoreService

	bootOkRan            bool
	bootRevisionsUpdated bool

	seedTimings *timings.Timings

	ensureSeedInConfigRan bool

	ensureInstalledRan bool

	ensureTriedRecoverySystemRan bool

	cloudInitAlreadyRestricted           bool
	cloudInitErrorAttemptStart           *time.Time
	cloudInitEnabledInactiveAttemptStart *time.Time

	lastBecomeOperationalAttempt time.Time
	becomeOperationalBackoff     time.Duration
	registered                   bool
	reg                          chan struct{}
	noRegister                   bool

	preseed             bool
	ntpSyncedOrTimedOut bool
}

// Manager returns a new device manager.
func Manager(s *state.State, hookManager *hookstate.HookManager, runner *state.TaskRunner, newStore func(storecontext.DeviceBackend) snapstate.StoreService) (*DeviceManager, error) {
	delayedCrossMgrInit()

	m := &DeviceManager{
		state:    s,
		hookMgr:  hookManager,
		newStore: newStore,
		reg:      make(chan struct{}),
		preseed:  snapdenv.Preseeding(),
	}

	modeEnv, err := maybeReadModeenv()
	if err != nil {
		return nil, err
	}
	if modeEnv != nil {
		m.sysMode = modeEnv.Mode
	}

	s.Lock()
	s.Cache(deviceMgrKey{}, m)
	s.Unlock()

	if err := m.confirmRegistered(); err != nil {
		return nil, err
	}

	hookManager.Register(regexp.MustCompile("^prepare-device$"), newBasicHookStateHandler)
	hookManager.Register(regexp.MustCompile("^install-device$"), newBasicHookStateHandler)

	runner.AddHandler("generate-device-key", m.doGenerateDeviceKey, nil)
	runner.AddHandler("request-serial", m.doRequestSerial, nil)
	runner.AddHandler("mark-preseeded", m.doMarkPreseeded, nil)
	runner.AddHandler("mark-seeded", m.doMarkSeeded, nil)
	runner.AddHandler("setup-run-system", m.doSetupRunSystem, nil)
	runner.AddHandler("restart-system-to-run-mode", m.doRestartSystemToRunMode, nil)
	runner.AddHandler("prepare-remodeling", m.doPrepareRemodeling, nil)
	runner.AddCleanup("prepare-remodeling", m.cleanupRemodel)
	// this *must* always run last and finalizes a remodel
	runner.AddHandler("set-model", m.doSetModel, nil)
	runner.AddCleanup("set-model", m.cleanupRemodel)
	// There is no undo for successful gadget updates. The system is
	// rebooted during update, if it boots up to the point where snapd runs
	// we deem the new assets (be it bootloader or firmware) functional. The
	// deployed boot assets must be backward compatible with reverted kernel
	// or gadget snaps. There are no further changes to the boot assets,
	// unless a new gadget update is deployed.
	runner.AddHandler("update-gadget-assets", m.doUpdateGadgetAssets, nil)
	// There is no undo handler for successful boot config update. The
	// config assets are assumed to be always backwards compatible.
	runner.AddHandler("update-managed-boot-config", m.doUpdateManagedBootConfig, nil)
	// kernel command line updates from a gadget supplied file
	runner.AddHandler("update-gadget-cmdline", m.doUpdateGadgetCommandLine, m.undoUpdateGadgetCommandLine)
	// recovery systems
	runner.AddHandler("create-recovery-system", m.doCreateRecoverySystem, m.undoCreateRecoverySystem)
	runner.AddHandler("finalize-recovery-system", m.doFinalizeTriedRecoverySystem, m.undoFinalizeTriedRecoverySystem)
	runner.AddCleanup("finalize-recovery-system", m.cleanupRecoverySystem)

	runner.AddBlocked(gadgetUpdateBlocked)

	// wire FDE kernel hook support into boot
	boot.HasFDESetupHook = m.hasFDESetupHook
	boot.RunFDESetupHook = m.runFDESetupHook
	hookManager.Register(regexp.MustCompile("^fde-setup$"), newFdeSetupHandler)

	return m, nil
}

type genericHook struct{}

func (h genericHook) Before() error                 { return nil }
func (h genericHook) Done() error                   { return nil }
func (h genericHook) Error(err error) (bool, error) { return false, nil }

func newBasicHookStateHandler(context *hookstate.Context) hookstate.Handler {
	return genericHook{}
}

func maybeReadModeenv() (*boot.Modeenv, error) {
	modeEnv, err := boot.ReadModeenv("")
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("cannot read modeenv: %v", err)
	}
	return modeEnv, nil
}

// ReloadModeenv is only useful for integration testing
func (m *DeviceManager) ReloadModeenv() error {
	osutil.MustBeTestBinary("ReloadModeenv can only be called from tests")
	modeEnv, err := maybeReadModeenv()
	if err != nil {
		return err
	}
	if modeEnv != nil {
		m.sysMode = modeEnv.Mode
	}
	return nil
}

type SysExpectation int

const (
	// SysAny indicates any system is appropriate.
	SysAny SysExpectation = iota
	// SysHasModeenv indicates only systems with modeenv are appropriate.
	SysHasModeenv
)

// SystemMode returns the current mode of the system.
// An expectation about the system controls the returned mode when
// none is set explicitly, as it's the case on pre-UC20 systems. In
// which case, with SysAny, the mode defaults to implicit "run", thus
// covering pre-UC20 systems. With SysHasModeeenv, as there is always
// an explicit mode in systems that use modeenv, no implicit default
// is used and thus "" is returned for pre-UC20 systems.
func (m *DeviceManager) SystemMode(sysExpect SysExpectation) string {
	if m.sysMode == "" {
		if sysExpect == SysHasModeenv {
			return ""
		}
		return "run"
	}
	return m.sysMode
}

// StartUp implements StateStarterUp.Startup.
func (m *DeviceManager) StartUp() error {
	// TODO:UC20: ubuntu-save needs to be mounted for recover too
	if !release.OnClassic && m.SystemMode(SysHasModeenv) == "run" {
		// UC20 and run mode => setup /var/lib/snapd/save
		if err := m.maybeSetupUbuntuSave(); err != nil {
			return fmt.Errorf("cannot set up ubuntu-save: %v", err)
		}
	}

	// TODO: setup proper timings measurements for this

	m.state.Lock()
	defer m.state.Unlock()
	return EarlyConfig(m.state, m.preloadGadget)
}

func (m *DeviceManager) maybeSetupUbuntuSave() error {
	// only called for UC20

	saveMounted, err := osutil.IsMounted(dirs.SnapSaveDir)
	if err != nil {
		return err
	}
	if saveMounted {
		logger.Noticef("save already mounted under %v", dirs.SnapSaveDir)
		m.saveAvailable = true
		return nil
	}

	runMntSaveMounted, err := osutil.IsMounted(boot.InitramfsUbuntuSaveDir)
	if err != nil {
		return err
	}
	if !runMntSaveMounted {
		// we don't have ubuntu-save, save will be used directly
		logger.Noticef("no ubuntu-save mount")
		m.saveAvailable = true
		return nil
	}

	logger.Noticef("bind-mounting ubuntu-save under %v", dirs.SnapSaveDir)

	err = systemd.New(systemd.SystemMode, progress.Null).Mount(boot.InitramfsUbuntuSaveDir,
		dirs.SnapSaveDir, "-o", "bind")
	if err != nil {
		logger.Noticef("bind-mounting ubuntu-save failed %v", err)
		return fmt.Errorf("cannot bind mount %v under %v: %v", boot.InitramfsUbuntuSaveDir, dirs.SnapSaveDir, err)
	}
	m.saveAvailable = true
	return nil
}

type deviceMgrKey struct{}

func deviceMgr(st *state.State) *DeviceManager {
	mgr := st.Cached(deviceMgrKey{})
	if mgr == nil {
		panic("internal error: device manager is not yet associated with state")
	}
	return mgr.(*DeviceManager)
}

func (m *DeviceManager) CanStandby() bool {
	var seeded bool
	if err := m.state.Get("seeded", &seeded); err != nil {
		return false
	}
	return seeded
}

func (m *DeviceManager) confirmRegistered() error {
	m.state.Lock()
	defer m.state.Unlock()

	device, err := m.device()
	if err != nil {
		return err
	}

	if device.Serial != "" {
		m.markRegistered()
	}
	return nil
}

func (m *DeviceManager) markRegistered() {
	if m.registered {
		return
	}
	m.registered = true
	close(m.reg)
}

func gadgetUpdateBlocked(cand *state.Task, running []*state.Task) bool {
	if cand.Kind() == "update-gadget-assets" && len(running) != 0 {
		// update-gadget-assets must be the only task running
		return true
	}
	for _, other := range running {
		if other.Kind() == "update-gadget-assets" {
			// no other task can be started when
			// update-gadget-assets is running
			return true
		}
	}

	return false
}

func (m *DeviceManager) changeInFlight(kind string) bool {
	for _, chg := range m.state.Changes() {
		if chg.Kind() == kind && !chg.Status().Ready() {
			// change already in motion
			return true
		}
	}
	return false
}

// helpers to keep count of attempts to get a serial, useful to decide
// to give up holding off trying to auto-refresh

type ensureOperationalAttemptsKey struct{}

func incEnsureOperationalAttempts(st *state.State) {
	cur, _ := st.Cached(ensureOperationalAttemptsKey{}).(int)
	st.Cache(ensureOperationalAttemptsKey{}, cur+1)
}

func ensureOperationalAttempts(st *state.State) int {
	cur, _ := st.Cached(ensureOperationalAttemptsKey{}).(int)
	return cur
}

// ensureOperationalShouldBackoff returns whether we should abstain from
// further become-operational tentatives while its backoff interval is
// not expired.
func (m *DeviceManager) ensureOperationalShouldBackoff(now time.Time) bool {
	if !m.lastBecomeOperationalAttempt.IsZero() && m.lastBecomeOperationalAttempt.Add(m.becomeOperationalBackoff).After(now) {
		return true
	}
	if m.becomeOperationalBackoff == 0 {
		m.becomeOperationalBackoff = 5 * time.Minute
	} else {
		newBackoff := m.becomeOperationalBackoff * 2
		if newBackoff > (12 * time.Hour) {
			newBackoff = 24 * time.Hour
		}
		m.becomeOperationalBackoff = newBackoff
	}
	m.lastBecomeOperationalAttempt = now
	return false
}

func setClassicFallbackModel(st *state.State, device *auth.DeviceState) error {
	err := assertstate.Add(st, sysdb.GenericClassicModel())
	if err != nil && !asserts.IsUnaccceptedUpdate(err) {
		return fmt.Errorf(`cannot install "generic-classic" fallback model assertion: %v`, err)
	}
	device.Brand = "generic"
	device.Model = "generic-classic"
	if err := internal.SetDevice(st, device); err != nil {
		return err
	}
	return nil
}

func (m *DeviceManager) ensureOperational() error {
	m.state.Lock()
	defer m.state.Unlock()

	if m.SystemMode(SysAny) != "run" {
		// avoid doing registration in ephemeral mode
		// note: this also stop auto-refreshes indirectly
		return nil
	}

	device, err := m.device()
	if err != nil {
		return err
	}

	if device.Serial != "" {
		// serial is set, we are all set
		return nil
	}

	perfTimings := timings.New(map[string]string{"ensure": "become-operational"})

	// conditions to trigger device registration
	//
	// * have a model assertion with a gadget (core and
	//   device-like classic) in which case we need also to wait
	//   for the gadget to have been installed though
	// TODO: consider a way to support lazy registration on classic
	// even with a gadget and some preseeded snaps
	//
	// * classic with a model assertion with a non-default store specified
	// * lazy classic case (might have a model with no gadget nor store
	//   or no model): we wait to have some snaps installed or be
	//   in the process to install some

	var seeded bool
	err = m.state.Get("seeded", &seeded)
	if err != nil && err != state.ErrNoState {
		return err
	}

	if device.Brand == "" || device.Model == "" {
		if !release.OnClassic || !seeded {
			return nil
		}
		// we are on classic and seeded but there is no model:
		// use a fallback model!
		err := setClassicFallbackModel(m.state, device)
		if err != nil {
			return err
		}
	}

	if m.noRegister {
		return nil
	}
	// noregister marker file is checked below after mostly in-memory checks

	if m.changeInFlight("become-operational") {
		return nil
	}

	var storeID, gadget string
	model, err := m.Model()
	if err != nil && err != state.ErrNoState {
		return err
	}
	if err == nil {
		gadget = model.Gadget()
		storeID = model.Store()
	} else {
		return fmt.Errorf("internal error: core device brand and model are set but there is no model assertion")
	}

	if gadget == "" && storeID == "" {
		// classic: if we have no gadget and no non-default store
		// wait to have snaps or snap installation

		n, err := snapstate.NumSnaps(m.state)
		if err != nil {
			return err
		}
		if n == 0 && !snapstate.Installing(m.state) {
			return nil
		}
	}

	// registration is blocked until reboot
	if osutil.FileExists(filepath.Join(dirs.SnapRunDir, "noregister")) {
		m.noRegister = true
		return nil
	}

	var hasPrepareDeviceHook bool
	// if there's a gadget specified wait for it
	if gadget != "" {
		// if have a gadget wait until seeded to proceed
		if !seeded {
			// this will be run again, so eventually when the system is
			// seeded the code below runs
			return nil

		}

		gadgetInfo, err := snapstate.CurrentInfo(m.state, gadget)
		if err != nil {
			return err
		}
		hasPrepareDeviceHook = (gadgetInfo.Hooks["prepare-device"] != nil)
	}

	// have some backoff between full retries
	if m.ensureOperationalShouldBackoff(time.Now()) {
		return nil
	}
	// increment attempt count
	incEnsureOperationalAttempts(m.state)

	// XXX: some of these will need to be split and use hooks
	// retries might need to embrace more than one "task" then,
	// need to be careful

	tasks := []*state.Task{}

	var prepareDevice *state.Task
	if hasPrepareDeviceHook {
		summary := i18n.G("Run prepare-device hook")
		hooksup := &hookstate.HookSetup{
			Snap: gadget,
			Hook: "prepare-device",
		}
		prepareDevice = hookstate.HookTask(m.state, summary, hooksup, nil)
		tasks = append(tasks, prepareDevice)
	}

	genKey := m.state.NewTask("generate-device-key", i18n.G("Generate device key"))
	if prepareDevice != nil {
		genKey.WaitFor(prepareDevice)
	}
	tasks = append(tasks, genKey)
	requestSerial := m.state.NewTask("request-serial", i18n.G("Request device serial"))
	requestSerial.WaitFor(genKey)
	tasks = append(tasks, requestSerial)

	chg := m.state.NewChange("become-operational", i18n.G("Initialize device"))
	chg.AddAll(state.NewTaskSet(tasks...))

	state.TagTimingsWithChange(perfTimings, chg)
	perfTimings.Save(m.state)

	return nil
}

var startTime time.Time

func init() {
	startTime = time.Now()
}

func (m *DeviceManager) setTimeOnce(name string, t time.Time) error {
	var prev time.Time
	err := m.state.Get(name, &prev)
	if err != nil && err != state.ErrNoState {
		return err
	}
	if !prev.IsZero() {
		// already set
		return nil
	}
	m.state.Set(name, t)
	return nil
}

func (m *DeviceManager) seedStart() (*timings.Timings, error) {
	if m.seedTimings != nil {
		// reuse the early cached one
		return m.seedTimings, nil
	}
	perfTimings := timings.New(map[string]string{"ensure": "seed"})

	var recordedStart string
	var start time.Time
	if m.preseed {
		recordedStart = "preseed-start-time"
		start = timeNow()
	} else {
		recordedStart = "seed-start-time"
		start = startTime
	}
	if err := m.setTimeOnce(recordedStart, start); err != nil {
		return nil, err
	}
	return perfTimings, nil
}

func (m *DeviceManager) preloadGadget() (sysconfig.Device, *gadget.Info, error) {
	var sysLabel string
	modeEnv, err := maybeReadModeenv()
	if err != nil {
		return nil, nil, err
	}
	if modeEnv != nil {
		sysLabel = modeEnv.RecoverySystem
	}

	// we time preloadGadget + first ensureSeeded together
	// under --ensure=seed
	tm, err := m.seedStart()
	if err != nil {
		return nil, nil, err
	}
	// cached for first ensureSeeded
	m.seedTimings = tm

	// Here we behave as if there was no gadget if we encounter
	// errors, under the assumption that those will be resurfaced
	// in ensureSeed. This preserves having a failing to seed
	// snapd continuing running.
	//
	// TODO: consider changing that again but we need consider the
	// effect of the different failure mode.
	//
	// We also assume that anything sensitive will not be guarded
	// just by option flags. For example automatic user creation
	// also requires the model to be known/set. Otherwise ignoring
	// errors here would be problematic.
	var deviceSeed seed.Seed
	timings.Run(tm, "import-assertions[early]", "early import assertions from seed", func(nested timings.Measurer) {
		deviceSeed, err = loadDeviceSeed(m.state, sysLabel)
	})
	if err != nil {
		// this same error will be resurfaced in ensureSeed later
		if err != seed.ErrNoAssertions {
			logger.Debugf("early import assertions from seed failed: %v", err)
		}
		return nil, nil, state.ErrNoState
	}
	model := deviceSeed.Model()
	if model.Gadget() == "" {
		// no gadget
		return nil, nil, state.ErrNoState
	}
	var gi *gadget.Info
	timings.Run(tm, "preload-verified-gadget-metadata", "preload verified gadget metadata from seed", func(nested timings.Measurer) {
		gi, err = func() (*gadget.Info, error) {
			if err := deviceSeed.LoadEssentialMeta([]snap.Type{snap.TypeGadget}, nested); err != nil {
				return nil, err
			}
			essGadget := deviceSeed.EssentialSnaps()
			if len(essGadget) != 1 {
				return nil, fmt.Errorf("multiple gadgets among essential snaps are unexpected")
			}
			snapf, err := snapfile.Open(essGadget[0].Path)
			if err != nil {
				return nil, err
			}
			return gadget.ReadInfoFromSnapFile(snapf, model)
		}()
	})
	if err != nil {
		logger.Noticef("preload verified gadget metadata from seed failed: %v", err)
		return nil, nil, state.ErrNoState
	}

	dev := newModelDeviceContext(m, model)
	return dev, gi, nil
}

var populateStateFromSeed = populateStateFromSeedImpl

// ensureSeeded makes sure that the snaps from seed.yaml get installed
// with the matching assertions
func (m *DeviceManager) ensureSeeded() error {
	m.state.Lock()
	defer m.state.Unlock()

	var seeded bool
	err := m.state.Get("seeded", &seeded)
	if err != nil && err != state.ErrNoState {
		return err
	}
	if seeded {
		return nil
	}

	if m.changeInFlight("seed") {
		return nil
	}

	perfTimings, err := m.seedStart()
	if err != nil {
		return err
	}
	// we time preloadGadget + first ensureSeeded together
	// succcessive ensureSeeded should be timed separately
	m.seedTimings = nil

	var opts *populateStateFromSeedOptions
	if m.preseed {
		opts = &populateStateFromSeedOptions{Preseed: true}
	} else {
		modeEnv, err := maybeReadModeenv()
		if err != nil {
			return err
		}
		if modeEnv != nil {
			opts = &populateStateFromSeedOptions{
				Mode:  modeEnv.Mode,
				Label: modeEnv.RecoverySystem,
			}
		}
	}

	var tsAll []*state.TaskSet
	timings.Run(perfTimings, "state-from-seed", "populate state from seed", func(tm timings.Measurer) {
		tsAll, err = populateStateFromSeed(m.state, opts, tm)
	})
	if err != nil {
		return err
	}
	if len(tsAll) == 0 {
		return nil
	}

	chg := m.state.NewChange("seed", "Initialize system state")
	for _, ts := range tsAll {
		chg.AddAll(ts)
	}
	m.state.EnsureBefore(0)

	state.TagTimingsWithChange(perfTimings, chg)
	perfTimings.Save(m.state)
	return nil
}

func (m *DeviceManager) ensureBootOk() error {
	m.state.Lock()
	defer m.state.Unlock()

	if release.OnClassic {
		return nil
	}

	// boot-ok/update-boot-revision is only relevant in run-mode
	if m.SystemMode(SysAny) != "run" {
		return nil
	}

	if !m.bootOkRan {
		deviceCtx, err := DeviceCtx(m.state, nil, nil)
		if err != nil && err != state.ErrNoState {
			return err
		}
		if err == nil {
			if err := boot.MarkBootSuccessful(deviceCtx); err != nil {
				return err
			}
		}
		m.bootOkRan = true
	}

	if !m.bootRevisionsUpdated {
		if err := snapstate.UpdateBootRevisions(m.state); err != nil {
			return err
		}
		m.bootRevisionsUpdated = true
	}

	return nil
}

func (m *DeviceManager) ensureCloudInitRestricted() error {
	m.state.Lock()
	defer m.state.Unlock()

	if m.cloudInitAlreadyRestricted {
		return nil
	}

	var seeded bool
	err := m.state.Get("seeded", &seeded)
	if err != nil && err != state.ErrNoState {
		return err
	}

	if !seeded {
		// we need to wait until we are seeded
		return nil
	}

	if release.OnClassic {
		// don't re-run on classic since classic devices don't get subject to
		// the cloud-init restricting that core devices do
		m.cloudInitAlreadyRestricted = true
		return nil
	}

	// On Ubuntu Core devices that have been seeded, we want to restrict
	// cloud-init so that its more dangerous (for an IoT device at least)
	// features are not exploitable after a device has been seeded. This allows
	// device administrators and other tools (such as multipass) to still
	// configure an Ubuntu Core device on first boot, and also allows cloud
	// vendors to run cloud-init with only a specific data-source on subsequent
	// boots but disallows arbitrary cloud-init {user,meta,vendor}-data to be
	// attached to a device via a USB drive and inject code onto the device.

	opts := &sysconfig.CloudInitRestrictOptions{}

	// check the current state of cloud-init, if it is disabled or already
	// restricted then we have nothing to do
	cloudInitStatus, err := cloudInitStatus()
	if err != nil {
		return err
	}
	statusMsg := ""

	switch cloudInitStatus {
	case sysconfig.CloudInitDisabledPermanently, sysconfig.CloudInitRestrictedBySnapd:
		// already been permanently disabled, nothing to do
		m.cloudInitAlreadyRestricted = true
		return nil
	case sysconfig.CloudInitNotFound:
		// no cloud init at all
		statusMsg = "not found"
	case sysconfig.CloudInitUntriggered:
		// hasn't been used
		statusMsg = "reported to be in disabled state"
	case sysconfig.CloudInitDone:
		// is done being used
		statusMsg = "reported to be done"
	case sysconfig.CloudInitErrored:
		// cloud-init errored, so we give the device admin / developer a few
		// minutes to reboot the machine to re-run cloud-init and try again,
		// otherwise we will disable cloud-init permanently

		// initialize the time we first saw cloud-init in error state
		if m.cloudInitErrorAttemptStart == nil {
			// save the time we started the attempt to restrict
			now := timeNow()
			m.cloudInitErrorAttemptStart = &now
			logger.Noticef("System initialized, cloud-init reported to be in error state, will disable in 3 minutes")
		}

		// check if 3 minutes have elapsed since we first saw cloud-init in
		// error state
		timeSinceFirstAttempt := timeNow().Sub(*m.cloudInitErrorAttemptStart)
		if timeSinceFirstAttempt <= 3*time.Minute {
			// we need to keep waiting for cloud-init, up to 3 minutes
			nextCheck := 3*time.Minute - timeSinceFirstAttempt
			m.state.EnsureBefore(nextCheck)
			return nil
		}
		// otherwise, we timed out waiting for cloud-init to be fixed or
		// rebooted and should restrict cloud-init
		// we will restrict cloud-init below, but we need to force the
		// disable, as by default RestrictCloudInit will error on state
		// CloudInitErrored
		opts.ForceDisable = true
		statusMsg = "reported to be in error state after 3 minutes"
	default:
		// in unknown states we are conservative and let the device run for
		// a while to see if it transitions to a known state, but eventually
		// will disable anyways
		fallthrough
	case sysconfig.CloudInitEnabled:
		// we will give cloud-init up to 5 minutes to try and run, if it
		// still has not transitioned to some other known state, then we
		// will give up waiting for it and disable it anyways

		// initialize the first time we saw cloud-init in enabled state
		if m.cloudInitEnabledInactiveAttemptStart == nil {
			// save the time we started the attempt to restrict
			now := timeNow()
			m.cloudInitEnabledInactiveAttemptStart = &now
		}

		// keep re-scheduling again in 10 seconds until we hit 5 minutes
		timeSinceFirstAttempt := timeNow().Sub(*m.cloudInitEnabledInactiveAttemptStart)
		if timeSinceFirstAttempt <= 5*time.Minute {
			// TODO: should we log a message here about waiting for cloud-init
			//       to be in a "known state"?
			m.state.EnsureBefore(10 * time.Second)
			return nil
		}

		// otherwise, we gave cloud-init 5 minutes to run, if it's still not
		// done disable it anyways
		// note we we need to force the disable, as by default
		// RestrictCloudInit will error on state CloudInitEnabled
		opts.ForceDisable = true
		statusMsg = "failed to transition to done or error state after 5 minutes"
	}

	// we should always have a model if we are seeded and are not on classic
	model, err := m.Model()
	if err != nil {
		return err
	}

	// For UC20, we want to always disable cloud-init after it has run on
	// first boot unless we are in a "real cloud", i.e. not using NoCloud,
	// or if we installed cloud-init configuration from the gadget
	if model.Grade() != asserts.ModelGradeUnset {
		// always disable NoCloud/local datasources after first boot on
		// uc20, this is because even if the gadget has a cloud.conf
		// configuring NoCloud, the config installed by cloud-init should
		// not work differently for later boots, so it's sufficient that
		// NoCloud runs on first-boot and never again
		opts.DisableAfterLocalDatasourcesRun = true
	}

	// now restrict/disable cloud-init
	res, err := restrictCloudInit(cloudInitStatus, opts)
	if err != nil {
		return err
	}

	// log a message about what we did
	actionMsg := ""
	switch res.Action {
	case "disable":
		actionMsg = "disabled permanently"
	case "restrict":
		// log different messages depending on what datasource was used
		if res.DataSource == "NoCloud" {
			actionMsg = "set datasource_list to [ NoCloud ] and disabled auto-import by filesystem label"
		} else {
			// all other datasources just log that we limited it to that datasource
			actionMsg = fmt.Sprintf("set datasource_list to [ %s ]", res.DataSource)
		}
	default:
		return fmt.Errorf("internal error: unexpected action %s taken while restricting cloud-init", res.Action)
	}
	logger.Noticef("System initialized, cloud-init %s, %s", statusMsg, actionMsg)

	m.cloudInitAlreadyRestricted = true

	return nil
}

func (m *DeviceManager) ensureInstalled() error {
	m.state.Lock()
	defer m.state.Unlock()

	if release.OnClassic {
		return nil
	}

	if m.ensureInstalledRan {
		return nil
	}

	if m.SystemMode(SysHasModeenv) != "install" {
		return nil
	}

	var seeded bool
	err := m.state.Get("seeded", &seeded)
	if err != nil && err != state.ErrNoState {
		return err
	}
	if !seeded {
		return nil
	}

	if m.changeInFlight("install-system") {
		return nil
	}

	perfTimings := timings.New(map[string]string{"ensure": "install-system"})

	model, err := m.Model()
	if err != nil && err != state.ErrNoState {
		return err
	}
	if err != nil {
		return fmt.Errorf("internal error: core device brand and model are set but there is no model assertion")
	}

	// check if the gadget has an install-device hook
	var hasInstallDeviceHook bool

	gadgetInfo, err := snapstate.CurrentInfo(m.state, model.Gadget())
	if err != nil {
		return fmt.Errorf("internal error: device is seeded in install mode but has no gadget snap: %v", err)
	}
	hasInstallDeviceHook = (gadgetInfo.Hooks["install-device"] != nil)

	m.ensureInstalledRan = true

	var prev *state.Task
	setupRunSystem := m.state.NewTask("setup-run-system", i18n.G("Setup system for run mode"))

	tasks := []*state.Task{setupRunSystem}
	addTask := func(t *state.Task) {
		t.WaitFor(prev)
		tasks = append(tasks, t)
		prev = t
	}
	prev = setupRunSystem

	// add the install-device hook before ensure-next-boot-to-run-mode if it
	// exists in the snap
	var installDevice *state.Task
	if hasInstallDeviceHook {
		summary := i18n.G("Run install-device hook")
		hooksup := &hookstate.HookSetup{
			// TODO: what's a reasonable timeout for the install-device hook?
			Snap: model.Gadget(),
			Hook: "install-device",
		}
		installDevice = hookstate.HookTask(m.state, summary, hooksup, nil)
		addTask(installDevice)
	}

	restartSystem := m.state.NewTask("restart-system-to-run-mode", i18n.G("Ensure next boot to run mode"))
	addTask(restartSystem)

	if installDevice != nil {
		// reference used by snapctl reboot
		installDevice.Set("restart-task", restartSystem.ID())
	}

	chg := m.state.NewChange("install-system", i18n.G("Install the system"))
	chg.AddAll(state.NewTaskSet(tasks...))

	state.TagTimingsWithChange(perfTimings, chg)
	perfTimings.Save(m.state)

	return nil
}

var timeNow = time.Now

// StartOfOperationTime returns the time when snapd started operating,
// and sets it in the state when called for the first time.
// The StartOfOperationTime time is seed-time if available,
// or current time otherwise.
func (m *DeviceManager) StartOfOperationTime() (time.Time, error) {
	var opTime time.Time
	if m.preseed {
		return opTime, fmt.Errorf("internal error: unexpected call to StartOfOperationTime in preseed mode")
	}
	err := m.state.Get("start-of-operation-time", &opTime)
	if err == nil {
		return opTime, nil
	}
	if err != nil && err != state.ErrNoState {
		return opTime, err
	}

	// start-of-operation-time not set yet, use seed-time if available
	var seedTime time.Time
	err = m.state.Get("seed-time", &seedTime)
	if err != nil && err != state.ErrNoState {
		return opTime, err
	}
	if err == nil {
		opTime = seedTime
	} else {
		opTime = timeNow()
	}
	m.state.Set("start-of-operation-time", opTime)
	return opTime, nil
}

func markSeededInConfig(st *state.State) error {
	var seedDone bool
	tr := config.NewTransaction(st)
	if err := tr.Get("core", "seed.loaded", &seedDone); err != nil && !config.IsNoOption(err) {
		return err
	}
	if !seedDone {
		if err := tr.Set("core", "seed.loaded", true); err != nil {
			return err
		}
		tr.Commit()
	}
	return nil
}

func (m *DeviceManager) ensureSeedInConfig() error {
	m.state.Lock()
	defer m.state.Unlock()

	if !m.ensureSeedInConfigRan {
		// get global seeded option
		var seeded bool
		if err := m.state.Get("seeded", &seeded); err != nil && err != state.ErrNoState {
			return err
		}
		if !seeded {
			// wait for ensure again, this is fine because
			// doMarkSeeded will run "EnsureBefore(0)"
			return nil
		}

		// Sync seeding with the configuration state. We need to
		// do this here to ensure that old systems which did not
		// set the configuration on seeding get the configuration
		// update too.
		if err := markSeededInConfig(m.state); err != nil {
			return err
		}
		m.ensureSeedInConfigRan = true
	}

	return nil

}

func (m *DeviceManager) appendTriedRecoverySystem(label string) error {
	// state is locked by the caller

	var triedSystems []string
	if err := m.state.Get("tried-systems", &triedSystems); err != nil && err != state.ErrNoState {
		return err
	}
	if strutil.ListContains(triedSystems, label) {
		// system already recorded as tried?
		return nil
	}
	triedSystems = append(triedSystems, label)
	m.state.Set("tried-systems", triedSystems)
	return nil
}

func (m *DeviceManager) ensureTriedRecoverySystem() error {
	if release.OnClassic {
		return nil
	}
	// nothing to do if not UC20 and run mode
	if m.SystemMode(SysHasModeenv) != "run" {
		return nil
	}
	if m.ensureTriedRecoverySystemRan {
		return nil
	}

	m.state.Lock()
	defer m.state.Unlock()

	deviceCtx, err := DeviceCtx(m.state, nil, nil)
	if err != nil && err != state.ErrNoState {
		return err
	}
	outcome, label, err := boot.InspectTryRecoverySystemOutcome(deviceCtx)
	if err != nil {
		if !boot.IsInconsistentRecoverySystemState(err) {
			return err
		}
		// boot variables state was inconsistent
		logger.Noticef("tried recovery system outcome error: %v", err)
	}
	switch outcome {
	case boot.TryRecoverySystemOutcomeSuccess:
		logger.Noticef("tried recovery system %q was successful", label)
		if err := m.appendTriedRecoverySystem(label); err != nil {
			return err
		}
	case boot.TryRecoverySystemOutcomeFailure:
		logger.Noticef("tried recovery system %q failed", label)
	case boot.TryRecoverySystemOutcomeInconsistent:
		logger.Noticef("inconsistent outcome of a tried recovery system")
	case boot.TryRecoverySystemOutcomeNoneTried:
		// no system was tried
	}
	if outcome != boot.TryRecoverySystemOutcomeNoneTried {
		if err := boot.ClearTryRecoverySystem(deviceCtx, label); err != nil {
			logger.Noticef("cannot clear tried recovery system status: %v", err)
			return err
		}
	}

	m.ensureTriedRecoverySystemRan = true
	return nil
}

type ensureError struct {
	errs []error
}

func (e *ensureError) Error() string {
	if len(e.errs) == 1 {
		return fmt.Sprintf("devicemgr: %v", e.errs[0])
	}
	parts := []string{"devicemgr:"}
	for _, e := range e.errs {
		parts = append(parts, e.Error())
	}
	return strings.Join(parts, "\n - ")
}

// no \n allowed in warnings
var seedFailureFmt = `seeding failed with: %v. This indicates an error in your distribution, please see https://forum.snapcraft.io/t/16341 for more information.`

// Ensure implements StateManager.Ensure.
func (m *DeviceManager) Ensure() error {
	var errs []error

	if err := m.ensureSeeded(); err != nil {
		m.state.Lock()
		m.state.Warnf(seedFailureFmt, err)
		m.state.Unlock()
		errs = append(errs, fmt.Errorf("cannot seed: %v", err))
	}

	if !m.preseed {
		if err := m.ensureCloudInitRestricted(); err != nil {
			errs = append(errs, err)
		}

		if err := m.ensureOperational(); err != nil {
			errs = append(errs, err)
		}

		if err := m.ensureBootOk(); err != nil {
			errs = append(errs, err)
		}

		if err := m.ensureSeedInConfig(); err != nil {
			errs = append(errs, err)
		}

		if err := m.ensureInstalled(); err != nil {
			errs = append(errs, err)
		}

		if err := m.ensureTriedRecoverySystem(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return &ensureError{errs}
	}

	return nil
}

// ResetToPostBootState is only useful for integration testing.
func (m *DeviceManager) ResetToPostBootState() {
	osutil.MustBeTestBinary("ResetToPostBootState can only be called from tests")
	m.bootOkRan = false
	m.bootRevisionsUpdated = false
	m.ensureTriedRecoverySystemRan = false
}

var errNoSaveSupport = errors.New("no save directory before UC20")

// withSaveDir invokes a function making sure save dir is available.
// Under UC16/18 it returns errNoSaveSupport
// For UC20 it also checks that ubuntu-save is available/mounted.
func (m *DeviceManager) withSaveDir(f func() error) error {
	// we use the model to check whether this is a UC20 device
	model, err := m.Model()
	if err == state.ErrNoState {
		return fmt.Errorf("internal error: cannot access save dir before a model is set")
	}
	if err != nil {
		return err
	}
	if model.Grade() == asserts.ModelGradeUnset {
		return errNoSaveSupport
	}
	// at this point we need save available
	if !m.saveAvailable {
		return fmt.Errorf("internal error: save dir is unavailable")
	}

	return f()
}

// withSaveAssertDB invokes a function making the save device assertion
// backup database available to it.
// Under UC16/18 it returns errNoSaveSupport
// For UC20 it also checks that ubuntu-save is available/mounted.
func (m *DeviceManager) withSaveAssertDB(f func(*asserts.Database) error) error {
	return m.withSaveDir(func() error {
		// open an ancillary backup assertion database in save/device
		assertDB, err := sysdb.OpenAt(dirs.SnapDeviceSaveDir)
		if err != nil {
			return err
		}
		return f(assertDB)
	})
}

// withKeypairMgr invokes a function making the device KeypairManager
// available to it.
// It uses the right location for the manager depending on UC16/18 vs 20,
// the latter uses ubuntu-save.
// For UC20 it also checks that ubuntu-save is available/mounted.
func (m *DeviceManager) withKeypairMgr(f func(asserts.KeypairManager) error) error {
	// we use the model to check whether this is a UC20 device
	// TODO: during a theoretical UC18->20 remodel the location of
	// keypair manager keys would move, we will need dedicated code
	// to deal with that, this code typically will return the old location
	// until a restart
	model, err := m.Model()
	if err == state.ErrNoState {
		return fmt.Errorf("internal error: cannot access device keypair manager before a model is set")
	}
	if err != nil {
		return err
	}
	underSave := false
	if model.Grade() != asserts.ModelGradeUnset {
		// on UC20 the keys are kept under the save dir
		underSave = true
	}
	where := dirs.SnapDeviceDir
	if underSave {
		// at this point we need save available
		if !m.saveAvailable {
			return fmt.Errorf("internal error: cannot access device keypair manager if ubuntu-save is unavailable")
		}
		where = dirs.SnapDeviceSaveDir
	}
	keypairMgr := m.cachedKeypairMgr
	if keypairMgr == nil {
		var err error
		keypairMgr, err = asserts.OpenFSKeypairManager(where)
		if err != nil {
			return err
		}
		m.cachedKeypairMgr = keypairMgr
	}
	return f(keypairMgr)
}

// TODO:UC20: we need proper encapsulated support to read
// tpm-policy-auth-key from save if the latter can get unmounted on
// demand

func (m *DeviceManager) keyPair() (asserts.PrivateKey, error) {
	device, err := m.device()
	if err != nil {
		return nil, err
	}

	if device.KeyID == "" {
		return nil, state.ErrNoState
	}

	var privKey asserts.PrivateKey
	err = m.withKeypairMgr(func(keypairMgr asserts.KeypairManager) (err error) {
		privKey, err = keypairMgr.Get(device.KeyID)
		if err != nil {
			return fmt.Errorf("cannot read device key pair: %v", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return privKey, nil
}

// Registered returns a channel that is closed when the device is known to have been registered.
func (m *DeviceManager) Registered() <-chan struct{} {
	return m.reg
}

type UnregisterOptions struct {
	NoRegistrationUntilReboot bool
}

// Unregister unregisters the device forgetting its serial
// plus the additional behavior described by the UnregisterOptions
func (m *DeviceManager) Unregister(opts *UnregisterOptions) error {
	device, err := m.device()
	if err != nil {
		return err
	}
	if !release.OnClassic || (device.Brand != "generic" && device.Brand != "canonical") {
		return fmt.Errorf("cannot currently unregister device if not classic or model brand is not generic or canonical")
	}

	if opts == nil {
		opts = &UnregisterOptions{}
	}
	if opts.NoRegistrationUntilReboot {
		if err := os.MkdirAll(dirs.SnapRunDir, 0755); err != nil {
			return err
		}
		if err := ioutil.WriteFile(filepath.Join(dirs.SnapRunDir, "noregister"), nil, 0644); err != nil {
			return err
		}
	}
	oldKeyID := device.KeyID
	device.Serial = ""
	device.KeyID = ""
	device.SessionMacaroon = ""
	if err := m.setDevice(device); err != nil {
		return err
	}
	// commit forgetting serial and key
	m.state.Unlock()
	m.state.Lock()
	// delete the device key
	err = m.withKeypairMgr(func(keypairMgr asserts.KeypairManager) error {
		err := keypairMgr.Delete(oldKeyID)
		if err != nil {
			return fmt.Errorf("cannot delete device key pair: %v", err)
		}
		return nil
	})

	m.lastBecomeOperationalAttempt = time.Time{}
	m.becomeOperationalBackoff = 0
	return err
}

// device returns current device state.
func (m *DeviceManager) device() (*auth.DeviceState, error) {
	return internal.Device(m.state)
}

// setDevice sets the device details in the state.
func (m *DeviceManager) setDevice(device *auth.DeviceState) error {
	return internal.SetDevice(m.state, device)
}

// Model returns the device model assertion.
func (m *DeviceManager) Model() (*asserts.Model, error) {
	return findModel(m.state)
}

// Serial returns the device serial assertion.
func (m *DeviceManager) Serial() (*asserts.Serial, error) {
	return findSerial(m.state, nil)
}

type SystemModeInfo struct {
	Mode              string
	HasModeenv        bool
	Seeded            bool
	BootFlags         []string
	HostDataLocations []string
}

// SystemModeInfo returns details about the current system mode the device is in.
func (m *DeviceManager) SystemModeInfo() (*SystemModeInfo, error) {
	deviceCtx, err := DeviceCtx(m.state, nil, nil)
	if err == state.ErrNoState {
		return nil, fmt.Errorf("cannot report system mode information before device model is acknowledged")
	}
	if err != nil {
		return nil, err
	}

	var seeded bool
	err = m.state.Get("seeded", &seeded)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}

	mode := deviceCtx.SystemMode()
	smi := SystemModeInfo{
		Mode:       mode,
		HasModeenv: deviceCtx.HasModeenv(),
		Seeded:     seeded,
	}
	if smi.HasModeenv {
		bootFlags, err := boot.BootFlags(deviceCtx)
		if err != nil {
			return nil, err
		}
		smi.BootFlags = bootFlags

		hostDataLocs, err := boot.HostUbuntuDataForMode(mode)
		if err != nil {
			return nil, err
		}
		smi.HostDataLocations = hostDataLocs
	}
	return &smi, nil
}

type SystemAction struct {
	Title string
	Mode  string
}

type System struct {
	// Current is true when the system running now was installed from that
	// seed
	Current bool
	// Label of the seed system
	Label string
	// Model assertion of the system
	Model *asserts.Model
	// Brand information
	Brand *asserts.Account
	// Actions available for this system
	Actions []SystemAction
}

var defaultSystemActions = []SystemAction{
	{Title: "Install", Mode: "install"},
}
var currentSystemActions = []SystemAction{
	{Title: "Reinstall", Mode: "install"},
	{Title: "Recover", Mode: "recover"},
	{Title: "Run normally", Mode: "run"},
}
var recoverSystemActions = []SystemAction{
	{Title: "Reinstall", Mode: "install"},
	{Title: "Run normally", Mode: "run"},
}

var ErrNoSystems = errors.New("no systems seeds")

// Systems list the available recovery/seeding systems. Returns the list of
// systems, ErrNoSystems when no systems seeds were found or other error.
func (m *DeviceManager) Systems() ([]*System, error) {
	// it's tough luck when we cannot determine the current system seed
	systemMode := m.SystemMode(SysAny)
	currentSys, _ := currentSystemForMode(m.state, systemMode)

	systemLabels, err := filepath.Glob(filepath.Join(dirs.SnapSeedDir, "systems", "*"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("cannot list available systems: %v", err)
	}
	if len(systemLabels) == 0 {
		// maybe not a UC20 system
		return nil, ErrNoSystems
	}

	var systems []*System
	for _, fpLabel := range systemLabels {
		label := filepath.Base(fpLabel)
		system, err := systemFromSeed(label, currentSys)
		if err != nil {
			// TODO:UC20 add a Broken field to the seed system like
			// we do for snap.Info
			logger.Noticef("cannot load system %q seed: %v", label, err)
			continue
		}
		systems = append(systems, system)
	}
	return systems, nil
}

var ErrUnsupportedAction = errors.New("unsupported action")

// Reboot triggers a reboot into the given systemLabel and mode.
//
// When called without a systemLabel and without a mode it will just
// trigger a regular reboot.
//
// When called without a systemLabel but with a mode it will use
// the current system to enter the given mode.
//
// Note that "recover" and "run" modes are only available for the
// current system.
func (m *DeviceManager) Reboot(systemLabel, mode string) error {
	rebootCurrent := func() {
		logger.Noticef("rebooting system")
		restart.Request(m.state, restart.RestartSystemNow)
	}

	// most simple case: just reboot
	if systemLabel == "" && mode == "" {
		m.state.Lock()
		defer m.state.Unlock()

		rebootCurrent()
		return nil
	}

	// no systemLabel means "current" so get the current system label
	if systemLabel == "" {
		systemMode := m.SystemMode(SysAny)
		currentSys, err := currentSystemForMode(m.state, systemMode)
		if err != nil {
			return fmt.Errorf("cannot get current system: %v", err)
		}
		systemLabel = currentSys.System
	}

	switched := func(systemLabel string, sysAction *SystemAction) {
		logger.Noticef("rebooting into system %q in %q mode", systemLabel, sysAction.Mode)
		restart.Request(m.state, restart.RestartSystemNow)
	}
	// even if we are already in the right mode we restart here by
	// passing rebootCurrent as this is what the user requested
	return m.switchToSystemAndMode(systemLabel, mode, rebootCurrent, switched)
}

// RequestSystemAction requests the provided system to be run in a
// given mode as specified by action.
// A system reboot will be requested when the request can be
// successfully carried out.
func (m *DeviceManager) RequestSystemAction(systemLabel string, action SystemAction) error {
	if systemLabel == "" {
		return fmt.Errorf("internal error: system label is unset")
	}

	nop := func() {}
	switched := func(systemLabel string, sysAction *SystemAction) {
		logger.Noticef("restarting into system %q for action %q", systemLabel, sysAction.Title)
		restart.Request(m.state, restart.RestartSystemNow)
	}
	// we do nothing (nop) if the mode and system are the same
	return m.switchToSystemAndMode(systemLabel, action.Mode, nop, switched)
}

// switchToSystemAndMode switches to given systemLabel and mode.
// If the systemLabel and mode are the same as current, it calls
// sameSystemAndMode. If successful otherwise it calls switched. Both
// are called with the state lock held.
func (m *DeviceManager) switchToSystemAndMode(systemLabel, mode string, sameSystemAndMode func(), switched func(systemLabel string, sysAction *SystemAction)) error {
	if err := checkSystemRequestConflict(m.state, systemLabel); err != nil {
		return err
	}

	systemMode := m.SystemMode(SysAny)
	// ignore the error to be robust in scenarios that
	// dont' stricly require currentSys to be carried through.
	// make sure that currentSys == nil does not break
	// the code below!
	// TODO: should we log the error?
	currentSys, _ := currentSystemForMode(m.state, systemMode)

	systemSeedDir := filepath.Join(dirs.SnapSeedDir, "systems", systemLabel)
	if _, err := os.Stat(systemSeedDir); err != nil {
		// XXX: should we wrap this instead return a naked stat error?
		return err
	}
	system, err := systemFromSeed(systemLabel, currentSys)
	if err != nil {
		return fmt.Errorf("cannot load seed system: %v", err)
	}

	var sysAction *SystemAction
	for _, act := range system.Actions {
		if mode == act.Mode {
			sysAction = &act
			break
		}
	}
	if sysAction == nil {
		// XXX: provide more context here like what mode was requested?
		return ErrUnsupportedAction
	}

	// XXX: requested mode is valid; only current system has 'run' and
	// recover 'actions'

	switch systemMode {
	case "recover", "run":
		// if going from recover to recover or from run to run and the systems
		// are the same do nothing
		if systemMode == sysAction.Mode && currentSys != nil && systemLabel == currentSys.System {
			m.state.Lock()
			defer m.state.Unlock()
			sameSystemAndMode()
			return nil
		}
	case "install":
		// requesting system actions in install mode does not make sense atm
		//
		// TODO:UC20: maybe factory hooks will be able to something like
		// this?
		return ErrUnsupportedAction
	default:
		// probably test device manager mocking problem, or also potentially
		// missing modeenv
		return fmt.Errorf("internal error: unexpected manager system mode %q", systemMode)
	}

	m.state.Lock()
	defer m.state.Unlock()

	deviceCtx, err := DeviceCtx(m.state, nil, nil)
	if err != nil {
		return err
	}
	if err := boot.SetRecoveryBootSystemAndMode(deviceCtx, systemLabel, mode); err != nil {
		return fmt.Errorf("cannot set device to boot into system %q in mode %q: %v", systemLabel, mode, err)
	}

	switched(systemLabel, sysAction)
	return nil
}

// implement storecontext.Backend

type storeContextBackend struct {
	*DeviceManager
}

func (scb storeContextBackend) Device() (*auth.DeviceState, error) {
	return scb.DeviceManager.device()
}

func (scb storeContextBackend) SetDevice(device *auth.DeviceState) error {
	return scb.DeviceManager.setDevice(device)
}

func (scb storeContextBackend) ProxyStore() (*asserts.Store, error) {
	st := scb.DeviceManager.state
	return proxyStore(st, config.NewTransaction(st))
}

// SignDeviceSessionRequest produces a signed device-session-request with for given serial assertion and nonce.
func (scb storeContextBackend) SignDeviceSessionRequest(serial *asserts.Serial, nonce string) (*asserts.DeviceSessionRequest, error) {
	if serial == nil {
		// shouldn't happen, but be safe
		return nil, fmt.Errorf("internal error: cannot sign a session request without a serial")
	}

	privKey, err := scb.DeviceManager.keyPair()
	if err == state.ErrNoState {
		return nil, fmt.Errorf("internal error: inconsistent state with serial but no device key")
	}
	if err != nil {
		return nil, err
	}

	a, err := asserts.SignWithoutAuthority(asserts.DeviceSessionRequestType, map[string]interface{}{
		"brand-id":  serial.BrandID(),
		"model":     serial.Model(),
		"serial":    serial.Serial(),
		"nonce":     nonce,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}, nil, privKey)
	if err != nil {
		return nil, err
	}

	return a.(*asserts.DeviceSessionRequest), nil
}

func (m *DeviceManager) StoreContextBackend() storecontext.Backend {
	return storeContextBackend{m}
}

var timeutilIsNTPSynchronized = timeutil.IsNTPSynchronized

func (m *DeviceManager) ntpSyncedOrWaitedLongerThan(maxWait time.Duration) bool {
	if m.ntpSyncedOrTimedOut {
		return true
	}
	if time.Now().After(startTime.Add(maxWait)) {
		logger.Noticef("no NTP sync after %v, trying auto-refresh anyway", maxWait)
		m.ntpSyncedOrTimedOut = true
		return true
	}

	var err error
	m.ntpSyncedOrTimedOut, err = timeutilIsNTPSynchronized()
	if errors.As(err, &timeutil.NoTimedate1Error{}) {
		// no timedate1 dbus service, no need to wait for it
		m.ntpSyncedOrTimedOut = true
		return true
	}
	if err != nil {
		logger.Debugf("cannot check if ntp is syncronized: %v", err)
	}

	return m.ntpSyncedOrTimedOut
}

func (m *DeviceManager) hasFDESetupHook() (bool, error) {
	// state must be locked
	st := m.state

	deviceCtx, err := DeviceCtx(st, nil, nil)
	if err != nil {
		return false, fmt.Errorf("cannot get device context: %v", err)
	}

	kernelInfo, err := snapstate.KernelInfo(st, deviceCtx)
	if err != nil {
		return false, fmt.Errorf("cannot get kernel info: %v", err)
	}
	return hasFDESetupHookInKernel(kernelInfo), nil
}

func (m *DeviceManager) runFDESetupHook(req *fde.SetupRequest) ([]byte, error) {
	// TODO:UC20: when this runs on refresh we need to be very careful
	// that we never run this when the kernel is not fully configured
	// i.e. when there are no security profiles for the hook

	// state must be locked
	st := m.state

	deviceCtx, err := DeviceCtx(st, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot get device context to run fde-setup hook: %v", err)
	}
	kernelInfo, err := snapstate.KernelInfo(st, deviceCtx)
	if err != nil {
		return nil, fmt.Errorf("cannot get kernel info to run fde-setup hook: %v", err)
	}
	hooksup := &hookstate.HookSetup{
		Snap:     kernelInfo.InstanceName(),
		Revision: kernelInfo.Revision,
		Hook:     "fde-setup",
		// XXX: should this be configurable somehow?
		Timeout: 5 * time.Minute,
	}
	contextData := map[string]interface{}{
		"fde-setup-request": req,
	}
	st.Unlock()
	defer st.Lock()
	context, err := m.hookMgr.EphemeralRunHook(context.Background(), hooksup, contextData)
	if err != nil {
		return nil, fmt.Errorf("cannot run hook for %q: %v", req.Op, err)
	}
	// the hook is expected to call "snapctl fde-setup-result" which
	// will set the "fde-setup-result" value on the task
	var hookOutput []byte
	context.Lock()
	err = context.Get("fde-setup-result", &hookOutput)
	context.Unlock()
	if err != nil {
		return nil, fmt.Errorf("cannot get result from fde-setup hook %q: %v", req.Op, err)
	}
	return hookOutput, nil
}

func (m *DeviceManager) checkFDEFeatures() (et secboot.EncryptionType, err error) {
	// Run fde-setup hook with "op":"features". If the hook
	// returns any {"features":[...]} reply we consider the
	// hardware supported. If the hook errors or if it returns
	// {"error":"hardware-unsupported"} we don't.
	features, err := fde.CheckFeatures(m.runFDESetupHook)
	if err != nil {
		return et, err
	}
	if strutil.ListContains(features, "device-setup") {
		et = secboot.EncryptionTypeDeviceSetupHook
	} else {
		et = secboot.EncryptionTypeLUKS
	}

	return et, nil
}

func hasFDESetupHookInKernel(kernelInfo *snap.Info) bool {
	_, ok := kernelInfo.Hooks["fde-setup"]
	return ok
}

type fdeSetupHandler struct {
	context *hookstate.Context
}

func newFdeSetupHandler(ctx *hookstate.Context) hookstate.Handler {
	return fdeSetupHandler{context: ctx}
}

func (h fdeSetupHandler) Before() error {
	return nil
}

func (h fdeSetupHandler) Done() error {
	return nil
}

func (h fdeSetupHandler) Error(err error) (bool, error) {
	return false, nil
}
