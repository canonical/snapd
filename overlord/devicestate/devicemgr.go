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

package devicestate

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/devicestate/internal"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/overlord/storecontext"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/timings"
)

// DeviceManager is responsible for managing the device identity and device
// policies.
type DeviceManager struct {
	state      *state.State
	keypairMgr asserts.KeypairManager

	// newStore can make new stores for remodeling
	newStore func(storecontext.DeviceBackend) snapstate.StoreService

	bootOkRan            bool
	bootRevisionsUpdated bool

	ensureSeedInConfigRan bool

	ensureInstalledRan bool

	lastBecomeOperationalAttempt time.Time
	becomeOperationalBackoff     time.Duration
	registered                   bool
	reg                          chan struct{}
	recoveryMode                 string
}

// Manager returns a new device manager.
func Manager(s *state.State, hookManager *hookstate.HookManager, runner *state.TaskRunner, newStore func(storecontext.DeviceBackend) snapstate.StoreService) (*DeviceManager, error) {
	delayedCrossMgrInit()

	keypairMgr, err := asserts.OpenFSKeypairManager(dirs.SnapDeviceDir)
	if err != nil {
		return nil, err

	}

	m := &DeviceManager{
		state:      s,
		keypairMgr: keypairMgr,
		newStore:   newStore,
		reg:        make(chan struct{}),
	}

	modeEnv, err := boot.ReadModeenv()
	if !os.IsNotExist(err) {
		return nil, err
	}
	if modeEnv != nil {
		m.recoveryMode = modeEnv.Mode
	}

	s.Lock()
	s.Cache(deviceMgrKey{}, m)
	s.Unlock()

	if err := m.confirmRegistered(); err != nil {
		return nil, err
	}

	hookManager.Register(regexp.MustCompile("^prepare-device$"), newPrepareDeviceHandler)

	runner.AddHandler("generate-device-key", m.doGenerateDeviceKey, nil)
	runner.AddHandler("request-serial", m.doRequestSerial, nil)
	runner.AddHandler("mark-seeded", m.doMarkSeeded, nil)
	runner.AddHandler("create-partitions", m.doCreatePartitions, nil)
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

	runner.AddBlocked(gadgetUpdateBlocked)

	return m, nil
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
	} else {
		for _, other := range running {
			if other.Kind() == "update-gadget-assets" {
				// no other task can be started when
				// update-gadget-assets is running
				return true
			}
		}
	}

	return false
}

type prepareDeviceHandler struct{}

func newPrepareDeviceHandler(context *hookstate.Context) hookstate.Handler {
	return prepareDeviceHandler{}
}

func (h prepareDeviceHandler) Before() error {
	return nil
}

func (h prepareDeviceHandler) Done() error {
	return nil
}

func (h prepareDeviceHandler) Error(err error) error {
	return nil
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

	perfTimings := timings.New(map[string]string{"ensure": "become-operational"})

	device, err := m.device()
	if err != nil {
		return err
	}

	if device.Serial != "" {
		// serial is set, we are all set
		return nil
	}

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
		// hooks are under a different manager, make sure we consider
		// it immediately
		m.state.EnsureBefore(0)
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

	perfTimings.AddTag("change-id", chg.ID())
	perfTimings.Save(m.state)

	return nil
}

var populateStateFromSeed = populateStateFromSeedImpl

// ensureSnaps makes sure that the snaps from seed.yaml get installed
// with the matching assertions
func (m *DeviceManager) ensureSeedYaml() error {
	m.state.Lock()
	defer m.state.Unlock()

	perfTimings := timings.New(map[string]string{"ensure": "seed"})

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

	// TODO: Core 20: how do we establish whether this is a Core 20
	// system, how do we receive mode here and also how to pick a label?
	var tsAll []*state.TaskSet
	timings.Run(perfTimings, "state-from-seed", "populate state from seed", func(tm timings.Measurer) {
		tsAll, err = populateStateFromSeed(m.state, nil, tm)
	})
	if err != nil {
		return err
	}
	if len(tsAll) == 0 {
		return nil
	}

	msg := fmt.Sprintf("Initialize system state")
	chg := m.state.NewChange("seed", msg)
	for _, ts := range tsAll {
		chg.AddAll(ts)
	}
	m.state.EnsureBefore(0)

	perfTimings.AddTag("change-id", chg.ID())
	perfTimings.Save(m.state)
	return nil
}

// ResetBootOk is only useful for integration testing
func (m *DeviceManager) ResetBootOk() {
	m.bootOkRan = false
	m.bootRevisionsUpdated = false
}

func (m *DeviceManager) ensureBootOk() error {
	m.state.Lock()
	defer m.state.Unlock()

	if release.OnClassic {
		return nil
	}

	if !m.bootOkRan {
		if err := boot.MarkBootSuccessful(); err != nil {
			return err
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

func (m *DeviceManager) ensureInstalled() error {
	m.state.Lock()
	defer m.state.Unlock()

	if release.OnClassic {
		return nil
	}

	if m.ensureInstalledRan {
		return nil
	}

	if m.recoveryMode != "install" {
		return nil
	}

	var seeded bool
	err := m.state.Get("seeded", &seeded)
	if err != nil {
		return err
	}
	if !seeded {
		return nil
	}

	if m.changeInFlight("install-system") {
		return nil
	}

	m.ensureInstalledRan = true

	tasks := []*state.Task{}
	createPartitions := m.state.NewTask("create-partitions", i18n.G("Create new partitions"))
	tasks = append(tasks, createPartitions)

	chg := m.state.NewChange("install-system", i18n.G("Install the system"))
	chg.AddAll(state.NewTaskSet(tasks...))

	return nil
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

// Ensure implements StateManager.Ensure.
func (m *DeviceManager) Ensure() error {
	var errs []error

	if err := m.ensureSeedYaml(); err != nil {
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

	if len(errs) > 0 {
		return &ensureError{errs}
	}

	return nil
}

func (m *DeviceManager) keyPair() (asserts.PrivateKey, error) {
	device, err := m.device()
	if err != nil {
		return nil, err
	}

	if device.KeyID == "" {
		return nil, state.ErrNoState
	}

	privKey, err := m.keypairMgr.Get(device.KeyID)
	if err != nil {
		return nil, fmt.Errorf("cannot read device key pair: %v", err)
	}
	return privKey, nil
}

// Registered returns a channel that is closed when the device is known to have been registered.
func (m *DeviceManager) Registered() <-chan struct{} {
	return m.reg
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
