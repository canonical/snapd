// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/context"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/partition"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

// DeviceManager is responsible for managing the device identity and device
// policies.
type DeviceManager struct {
	state      *state.State
	keypairMgr asserts.KeypairManager
	runner     *state.TaskRunner

	bootOkRan            bool
	bootRevisionsUpdated bool

	lastBecomeOperationalAttempt time.Time
	becomeOperationalBackoff     time.Duration
	registered                   bool
	reg                          chan struct{}
	registrationFirstAttempted   bool
	regFirstAttempt              chan struct{}
}

// Manager returns a new device manager.
func Manager(s *state.State, hookManager *hookstate.HookManager) (*DeviceManager, error) {
	delayedCrossMgrInit()

	runner := state.NewTaskRunner(s)

	keypairMgr, err := asserts.OpenFSKeypairManager(dirs.SnapDeviceDir)
	if err != nil {
		return nil, err

	}

	m := &DeviceManager{
		state:           s,
		keypairMgr:      keypairMgr,
		runner:          runner,
		reg:             make(chan struct{}),
		regFirstAttempt: make(chan struct{}),
	}

	if err := m.confirmRegistered(); err != nil {
		return nil, err
	}

	hookManager.Register(regexp.MustCompile("^prepare-device$"), newPrepareDeviceHandler)

	runner.AddHandler("generate-device-key", m.doGenerateDeviceKey, nil)
	runner.AddHandler("request-serial", m.doRequestSerial, nil)
	runner.AddHandler("mark-seeded", m.doMarkSeeded, nil)

	return m, nil
}

func (m *DeviceManager) confirmRegistered() error {
	m.state.Lock()
	defer m.state.Unlock()

	device, err := auth.Device(m.state)
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

func (m *DeviceManager) markRegistrationFirstAttempt() {
	if m.registrationFirstAttempted {
		return
	}
	if !m.registered {
		// TODO: this should be a system warning
		logger.Noticef("first device registration attempt did not succeed, registration will be retried but some operations might fail until the device is registered")

	}
	m.registrationFirstAttempted = true
	close(m.regFirstAttempt)
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

func (m *DeviceManager) ensureOperationalResetAttempts() {
	m.state.Cache(ensureOperationalAttemptsKey{}, 0)
	m.becomeOperationalBackoff = 0
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

func (m *DeviceManager) ensureOperational() error {
	m.state.Lock()
	defer m.state.Unlock()

	device, err := auth.Device(m.state)
	if err != nil {
		return err
	}

	if device.Serial != "" {
		// serial is set, we are all set
		return nil
	}

	// triggering device registration
	//
	// * have a model assertion
	// * on classic if we are seeded but don't have a model,
	//   we setup the fallback generic/generic-classic
	// * with a model assertion with a gadget (core and device-like
	//   classic) we wait for the gadget to have been installed
	// * otherwise we proceed
	// * then on classic we pause registration until the first
	//   store interaction (registration-paused flag in state and
	//   code in doRequestSerial)
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
		err = assertstate.Add(m.state, sysdb.GenericClassicModel())
		if err != nil && !asserts.IsUnaccceptedUpdate(err) {
			return fmt.Errorf(`cannot install "generic-classic" fallback model assertion: %v`, err)
		}
		device.Brand = "generic"
		device.Model = "generic-classic"
		if err := auth.SetDevice(m.state, device); err != nil {
			return err
		}
	}

	if m.changeInFlight("become-operational") {
		return nil
	}

	var gadget string
	model, err := Model(m.state)
	if err != nil && err != state.ErrNoState {
		return err
	}
	if err == nil {
		gadget = model.Gadget()
	} else {
		return fmt.Errorf("internal error: device brand and model are set but there is no model assertion")
	}

	// if there's a gadget specified wait for it
	var gadgetInfo *snap.Info
	if gadget != "" {
		var err error
		gadgetInfo, err = snapstate.GadgetInfo(m.state)
		if err == state.ErrNoState {
			// no gadget installed yet, cannot proceed
			return nil
		}
		if err != nil {
			return err
		}
	}

	full := true // do all: prepare-device + key gen + request serial
	if release.OnClassic {
		// paused?
		var paused bool
		err := m.state.Get("registration-paused", &paused)
		if err != nil && err != state.ErrNoState {
			return err
		}
		if paused {
			return nil
		}
		// on classic the first time around we just do
		// prepare-device + key gen and then pause the process
		// until the first store interaction
		full = err != state.ErrNoState
	}

	if full {
		// have some backoff between full retries
		if m.ensureOperationalShouldBackoff(time.Now()) {
			return nil
		}
		// increment attempt count
		incEnsureOperationalAttempts(m.state)
	}

	tasks := []*state.Task{}

	var prepareDevice *state.Task
	if gadgetInfo != nil && gadgetInfo.Hooks["prepare-device"] != nil {
		summary := i18n.G("Run prepare-device hook")
		hooksup := &hookstate.HookSetup{
			Snap: gadgetInfo.Name(),
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

	if full {
		requestSerial := m.state.NewTask("request-serial", i18n.G("Request device serial"))
		requestSerial.WaitFor(genKey)
		tasks = append(tasks, requestSerial)
	}

	msg := i18n.G("Initialize device")
	if !full {
		// just prepare-device + key gen
		msg = i18n.G("Prepare device")
	}
	chg := m.state.NewChange("become-operational", msg)
	chg.AddAll(state.NewTaskSet(tasks...))

	if !full {
		m.state.Set("registration-paused", true)
	}

	return nil
}

// waitForRegistrationTimeout is the timeout after which waitForRegistration will give up, about the same as network retries max timeout
var waitForRegistrationTimeout = 30 * time.Second

// waitForRegistration does a best-effort of waiting for registration to occur.
// Assumes it hasn't happened yet and that state is locked.
func (m *DeviceManager) waitForRegistration(ctx context.Context) (*asserts.Serial, error) {
	m.state.Set("registration-paused", false)
	m.state.EnsureBefore(0)
	if auth.IsEnsureContext(ctx) {
		return nil, state.ErrNoState
	}
	if ensureOperationalAttempts(m.state) > 1 {
		// we are in the subsequent retries phase, do not wait
		return nil, state.ErrNoState
	}
	m.state.Unlock()
	select {
	case <-m.Registered():
		// registered
	case <-m.regFirstAttempt:
		// first registration attempt finished (successfully or not)
	case <-time.After(waitForRegistrationTimeout):
		// neither, timed out
	}
	m.state.Lock()

	// mark first attempt here as well in case we failed earlier
	// than normally expected, and reached the timeout
	m.markRegistrationFirstAttempt()

	return Serial(m.state)
}

var populateStateFromSeed = populateStateFromSeedImpl

// ensureSnaps makes sure that the snaps from seed.yaml get installed
// with the matching assertions
func (m *DeviceManager) ensureSeedYaml() error {
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

	tsAll, err := populateStateFromSeed(m.state)
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

	return nil
}

func (m *DeviceManager) ensureBootOk() error {
	m.state.Lock()
	defer m.state.Unlock()

	if release.OnClassic {
		return nil
	}

	if !m.bootOkRan {
		bootloader, err := partition.FindBootloader()
		if err != nil {
			return fmt.Errorf(i18n.G("cannot mark boot successful: %s"), err)
		}
		if err := partition.MarkBootSuccessful(bootloader); err != nil {
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

func (m *DeviceManager) KnownTaskKinds() []string {
	return m.runner.KnownTaskKinds()
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

	m.runner.Ensure()

	if len(errs) > 0 {
		return &ensureError{errs}
	}

	return nil
}

// Wait implements StateManager.Wait.
func (m *DeviceManager) Wait() {
	m.runner.Wait()
}

// Stop implements StateManager.Stop.
func (m *DeviceManager) Stop() {
	m.runner.Stop()
}

func (m *DeviceManager) keyPair() (asserts.PrivateKey, error) {
	device, err := auth.Device(m.state)
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

// implementing auth.DeviceAssertions
// sanity check
var _ auth.DeviceAssertions = (*DeviceManager)(nil)

// Model returns the device model assertion.
func (m *DeviceManager) Model() (*asserts.Model, error) {
	m.state.Lock()
	defer m.state.Unlock()

	return Model(m.state)
}

// Serial returns the device serial assertion.
func (m *DeviceManager) Serial() (*asserts.Serial, error) {
	m.state.Lock()
	defer m.state.Unlock()

	return Serial(m.state)
}

// Registered returns a channel that is closed when the device is known to have been registered.
func (m *DeviceManager) Registered() <-chan struct{} {
	return m.reg
}

// DeviceSessionRequestParams produces a device-session-request with the given nonce, together with other required parameters, the device serial and model assertions.
func (m *DeviceManager) DeviceSessionRequestParams(ctx context.Context, nonce string) (*auth.DeviceSessionRequestParams, error) {
	m.state.Lock()
	defer m.state.Unlock()

	model, err := Model(m.state)
	if err != nil {
		return nil, err
	}

	serial, err := Serial(m.state)
	if err != nil {
		// do a best effort to wait for registration
		serial, err = m.waitForRegistration(ctx)
		if err != nil {
			return nil, err
		}
	}

	privKey, err := m.keyPair()
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

	return &auth.DeviceSessionRequestParams{
		Request: a.(*asserts.DeviceSessionRequest),
		Serial:  serial,
		Model:   model,
	}, err

}

// ProxyStore returns the store assertion for the proxy store if one is set.
func (m *DeviceManager) ProxyStore() (*asserts.Store, error) {
	m.state.Lock()
	defer m.state.Unlock()

	return ProxyStore(m.state)
}
