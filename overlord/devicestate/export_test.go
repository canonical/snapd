// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2024 Canonical Ltd
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
	"net/http"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/gadget/install"
	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/kernel/fde"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/user"
	"github.com/snapcore/snapd/overlord/fdestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/overlord/storecontext"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/sysconfig"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
)

var (
	SystemForPreseeding         = systemForPreseeding
	GetUserDetailsFromAssertion = getUserDetailsFromAssertion
	ShouldRequestSerial         = shouldRequestSerial
)

func MockKeyLength(n int) (restore func()) {
	if n < 1024 {
		panic("key length must be >= 1024")
	}

	oldKeyLength := keyLength
	keyLength = n
	return func() {
		keyLength = oldKeyLength
	}
}

func MockBaseStoreURL(url string) (restore func()) {
	oldURL := baseStoreURL
	baseStoreURL = mustParse(url).ResolveReference(authRef)
	return func() {
		baseStoreURL = oldURL
	}
}

func MockRetryInterval(interval time.Duration) (restore func()) {
	old := retryInterval
	retryInterval = interval
	return func() {
		retryInterval = old
	}
}

func MockMaxTentatives(max int) (restore func()) {
	old := maxTentatives
	maxTentatives = max
	return func() {
		maxTentatives = old
	}
}

func MockTimeNow(f func() time.Time) (restore func()) {
	old := timeNow
	timeNow = f
	return func() {
		timeNow = old
	}
}

func KeypairManager(m *DeviceManager) (keypairMgr asserts.KeypairManager) {
	// XXX expose the with... method at some point
	err := m.withKeypairMgr(func(km asserts.KeypairManager) error {
		keypairMgr = km
		return nil
	})
	if err != nil {
		panic(err)
	}
	return keypairMgr
}

func SaveAvailable(m *DeviceManager) bool {
	return m.saveAvailable
}

func SetSaveAvailable(m *DeviceManager, avail bool) {
	m.saveAvailable = avail
}

func EnsureOperationalShouldBackoff(m *DeviceManager, now time.Time) bool {
	return m.ensureOperationalShouldBackoff(now)
}

func LoadSystemAndEssentialSnaps(m *DeviceManager, wantedSystemLabel string, types []snap.Type, modeForComps string) (*systemAndEssentialSnaps, error) {
	return m.loadSystemAndEssentialSnaps(wantedSystemLabel, types, modeForComps)
}

func BecomeOperationalBackoff(m *DeviceManager) time.Duration {
	return m.becomeOperationalBackoff
}

func SetLastBecomeOperationalAttempt(m *DeviceManager, t time.Time) {
	m.lastBecomeOperationalAttempt = t
}

func SetSystemMode(m *DeviceManager, mode string) {
	m.sysMode = mode
}

func GetSystemMode(m *DeviceManager) string {
	return m.sysMode
}

func SetTimeOnce(m *DeviceManager, name string, t time.Time) error {
	return m.setTimeOnce(name, t)
}

func EarlyPreloadGadget(m *DeviceManager) (sysconfig.Device, *gadget.Info, error) {
	// let things fully run again
	m.earlyDeviceSeed = nil
	return m.earlyPreloadGadget()
}

func MockLoadDeviceSeed(f func(st *state.State, sysLabel string) (seed.Seed, error)) func() {
	old := loadDeviceSeed
	loadDeviceSeed = f
	return func() {
		loadDeviceSeed = old
	}
}

func MockRepeatRequestSerial(label string) (restore func()) {
	old := repeatRequestSerial
	repeatRequestSerial = label
	return func() {
		repeatRequestSerial = old
	}
}

func MockSnapstateUpdateOne(mock func(ctx context.Context, st *state.State, goal snapstate.UpdateGoal, filter func(*snap.Info, *snapstate.SnapState) bool, opts snapstate.Options) (*state.TaskSet, error)) (restore func()) {
	return testutil.Mock(&snapstateUpdateOne, mock)
}

func MockSnapstateInstallOne(mock func(ctx context.Context, st *state.State, goal snapstate.InstallGoal, opts snapstate.Options) (*snap.Info, *state.TaskSet, error)) (restore func()) {
	return testutil.Mock(&snapstateInstallOne, mock)
}

func MockSnapstatePathUpdateGoal(mock func(snaps ...snapstate.PathSnap) snapstate.UpdateGoal) (restore func()) {
	return testutil.Mock(&snapstatePathUpdateGoal, mock)
}

func MockSnapstateStoreInstallGoal(mock func(snaps ...snapstate.StoreSnap) snapstate.InstallGoal) (restore func()) {
	return testutil.Mock(&snapstateStoreInstallGoal, mock)
}

func MockSnapstateStoreUpdateGoal(mock func(snaps ...snapstate.StoreUpdate) snapstate.UpdateGoal) (restore func()) {
	return testutil.Mock(&snapstateStoreUpdateGoal, mock)
}

func MockSnapstatePathInstallGoal(mock func(snapstate.PathSnap) snapstate.InstallGoal) (restore func()) {
	return testutil.Mock(&snapstatePathInstallGoal, mock)
}

func MockSnapstateInstallComponents(mock func(ctx context.Context, st *state.State, names []string, info *snap.Info, vsets *snapasserts.ValidationSets, opts snapstate.Options) ([]*state.TaskSet, error)) (restore func()) {
	return testutil.Mock(&snapstateInstallComponents, mock)
}

func MockSnapstateInstallComponentPath(mock func(st *state.State, csi *snap.ComponentSideInfo, info *snap.Info, path string, opts snapstate.Options) (*state.TaskSet, error)) (restore func()) {
	return testutil.Mock(&snapstateInstallComponentPath, mock)
}

func MockSnapstateDownload(f func(ctx context.Context, st *state.State, name string, components []string, blobDirectory string, revOpts snapstate.RevisionOptions, opts snapstate.Options) (*state.TaskSet, *snap.Info, error)) (restore func()) {
	r := testutil.Backup(&snapstateDownload)
	snapstateDownload = f
	return r
}

func MockSnapstateDownloadComponents(f func(ctx context.Context, st *state.State, name string, components []string, blobDirectory string, revOpts snapstate.RevisionOptions, opts snapstate.Options) (*state.TaskSet, error)) (restore func()) {
	r := testutil.Backup(&snapstateDownloadComponents)
	snapstateDownloadComponents = f
	return r
}

func EnsureSeeded(m *DeviceManager) error {
	return m.ensureSeeded()
}

func EnsureCloudInitRestricted(m *DeviceManager) error {
	return m.ensureCloudInitRestricted()
}

func EnsureSerialBoundSystemUserAssertionsProcessed(m *DeviceManager) error {
	return m.ensureSerialBoundSystemUserAssertionsProcessed()
}

func ImportAssertionsFromSeed(m *DeviceManager, mode string, isCoreBoot bool) (seed.Seed, error) {
	return m.importAssertionsFromSeed(mode, isCoreBoot)
}

func PopulateStateFromSeedImpl(m *DeviceManager, tm timings.Measurer) ([]*state.TaskSet, error) {
	return m.populateStateFromSeedImpl(tm)
}

func MockPopulateStateFromSeed(m *DeviceManager, f func(seedLabel, seedMode string, tm timings.Measurer) ([]*state.TaskSet, error)) (restore func()) {
	old := m.populateStateFromSeed
	m.populateStateFromSeed = func(tm timings.Measurer) ([]*state.TaskSet, error) {
		sLabel, sMode, err := m.seedLabelAndMode()
		if err != nil {
			panic(err)
		}
		return f(sLabel, sMode, tm)
	}
	return func() {
		m.populateStateFromSeed = old
	}
}

func EnsureAutoImportAssertions(m *DeviceManager) error {
	return m.ensureAutoImportAssertions()
}

func ReloadEarlyDeviceSeed(m *DeviceManager, seedLoadErr error) (snapstate.DeviceContext, seed.Seed, error) {
	m.seedChosen = false
	return m.earlyLoadDeviceSeed(seedLoadErr)
}

func MockProcessAutoImportAssertion(f func(*state.State, seed.Seed, asserts.RODatabase, func(batch *asserts.Batch) error) error) (restore func()) {
	old := processAutoImportAssertionsImpl
	processAutoImportAssertionsImpl = f
	return func() {
		processAutoImportAssertionsImpl = old
	}
}

func EnsureBootOk(m *DeviceManager) error {
	return m.ensureBootOk()
}

func SetBootOkRan(m *DeviceManager, b bool) {
	m.bootOkRan = b
}

func SetBootRevisionsUpdated(m *DeviceManager, b bool) {
	m.bootRevisionsUpdated = b
}

func SetInstalledRan(m *DeviceManager, b bool) {
	m.ensureInstalledRan = b
}

func SetTriedSystemsRan(m *DeviceManager, b bool) {
	m.ensureTriedRecoverySystemRan = b
}

func SetPostFactoryResetRan(m *DeviceManager, b bool) {
	m.ensurePostFactoryResetRan = b
}

func StartTime() time.Time {
	return startTime
}

type (
	RegistrationContext = registrationContext
	RemodelContext      = remodelContext
	SeededSystem        = seededSystem
	RecoverySystemSetup = recoverySystemSetup
)

func RegistrationCtx(m *DeviceManager, t *state.Task) (registrationContext, error) {
	return m.registrationCtx(t)
}

func RemodelDeviceBackend(remodCtx remodelContext) storecontext.DeviceBackend {
	return remodCtx.(interface {
		deviceBackend() storecontext.DeviceBackend
	}).deviceBackend()
}

func RemodelSetRecoverySystemLabel(remodCtx remodelContext, label string) {
	remodCtx.setRecoverySystemLabel(label)
}

func RecordSeededSystem(m *DeviceManager, st *state.State, sys *seededSystem) error {
	return m.recordSeededSystem(st, sys)
}

var (
	LoadDeviceSeed               = loadDeviceSeed
	CheckGadgetOrKernel          = checkGadgetOrKernel
	CheckGadgetValid             = checkGadgetValid
	CheckGadgetRemodelCompatible = checkGadgetRemodelCompatible
	CanAutoRefresh               = canAutoRefresh
	NewEnoughProxy               = newEnoughProxy

	IncEnsureOperationalAttempts = incEnsureOperationalAttempts
	EnsureOperationalAttempts    = ensureOperationalAttempts

	RemodelTasks = remodelTasks

	RemodelCtx        = remodelCtx
	CleanupRemodelCtx = cleanupRemodelCtx
	CachedRemodelCtx  = cachedRemodelCtx

	GadgetUpdateBlocked = gadgetUpdateBlocked
	PendingGadgetInfo   = pendingGadgetData

	CriticalTaskEdges = criticalTaskEdges

	CreateSystemForModelFromValidatedSnaps = createSystemForModelFromValidatedSnaps
	LogNewSystemSnapFile                   = logNewSystemSnapFile
	PurgeNewSystemSnapFiles                = purgeNewSystemSnapFiles
	CreateRecoverySystemTasks              = createRecoverySystemTasks
)

func MockApplyPreseededData(f func(deviceSeed seed.PreseedCapable, writableDir string) error) (restore func()) {
	r := testutil.Backup(&applyPreseededData)
	applyPreseededData = f
	return r
}

func MockSeedOpen(f func(seedDir, label string) (seed.Seed, error)) (restore func()) {
	r := testutil.Backup(&seedOpen)
	seedOpen = f
	return r
}

func MockGadgetUpdate(mock func(model gadget.Model, current, update gadget.GadgetData, path string, policy gadget.UpdatePolicyFunc, observer gadget.ContentUpdateObserver) error) (restore func()) {
	old := gadgetUpdate
	gadgetUpdate = mock
	return func() {
		gadgetUpdate = old
	}
}

func MockGadgetIsCompatible(mock func(current, update *gadget.Info) error) (restore func()) {
	old := gadgetIsCompatible
	gadgetIsCompatible = mock
	return func() {
		gadgetIsCompatible = old
	}
}

func MockBootMakeSystemRunnable(f func(model *asserts.Model, bootWith *boot.BootableSet, obs boot.TrustedAssetsInstallObserver) error) (restore func()) {
	restore = testutil.Backup(&bootMakeRunnable)
	bootMakeRunnable = f
	return restore
}

func MockBootMakeSystemRunnableAfterDataReset(f func(model *asserts.Model, bootWith *boot.BootableSet, obs boot.TrustedAssetsInstallObserver) error) (restore func()) {
	restore = testutil.Backup(&bootMakeRunnableAfterDataReset)
	bootMakeRunnableAfterDataReset = f
	return restore
}

func MockBootMakeRecoverySystemBootable(f func(model *asserts.Model, rootdir string, relativeRecoverySystemDir string, bootWith *boot.RecoverySystemBootableSet) error) (restore func()) {
	restore = testutil.Backup(&bootMakeRecoverySystemBootable)
	bootMakeRecoverySystemBootable = f
	return restore
}

func MockBootEnsureNextBootToRunMode(f func(systemLabel string) error) (restore func()) {
	old := bootEnsureNextBootToRunMode
	bootEnsureNextBootToRunMode = f
	return func() {
		bootEnsureNextBootToRunMode = old
	}
}

func MockHttputilNewHTTPClient(f func(opts *httputil.ClientOptions) *http.Client) (restore func()) {
	old := httputilNewHTTPClient
	httputilNewHTTPClient = f
	return func() {
		httputilNewHTTPClient = old
	}
}

func MockInstallLogicPrepareRunSystemData(f func(mod *asserts.Model, gadgetDir string, _ timings.Measurer) error) (restore func()) {
	r := testutil.Backup(&installLogicPrepareRunSystemData)
	installLogicPrepareRunSystemData = f
	return r
}

func MockInstallRun(f func(model gadget.Model, gadgetRoot string, kernelSnapInfo *install.KernelSnapInfo, device string, options install.Options, observer gadget.ContentObserver, perfTimings timings.Measurer) (*install.InstalledSystemSideData, error)) (restore func()) {
	old := installRun
	installRun = f
	return func() {
		installRun = old
	}
}

func MockInstallFactoryReset(f func(model gadget.Model, gadgetRoot string, kernelSnapInfo *install.KernelSnapInfo, device string, options install.Options, observer gadget.ContentObserver, perfTimings timings.Measurer) (*install.InstalledSystemSideData, error)) (restore func()) {
	restore = testutil.Backup(&installFactoryReset)
	installFactoryReset = f
	return restore
}

func MockInstallWriteContent(f func(onVolumes map[string]*gadget.Volume, allLaidOutVols map[string]*gadget.LaidOutVolume, encSetupData *install.EncryptionSetupData, kSnapInfo *install.KernelSnapInfo, observer gadget.ContentObserver, perfTimings timings.Measurer) ([]*gadget.OnDiskVolume, error)) (restore func()) {
	old := installWriteContent
	installWriteContent = f
	return func() {
		installWriteContent = old
	}
}

func MockInstallMountVolumes(f func(onVolumes map[string]*gadget.Volume, encSetupData *install.EncryptionSetupData) (espMntDir string, unmount func() error, err error)) (restore func()) {
	old := installMountVolumes
	installMountVolumes = f
	return func() {
		installMountVolumes = old
	}
}

func MockInstallEncryptPartitions(f func(onVolumes map[string]*gadget.Volume, volumesAuth *device.VolumesAuthOptions, encryptionType device.EncryptionType, model *asserts.Model, gadgetRoot, kernelRoot string, perfTimings timings.Measurer) (*install.EncryptionSetupData, error)) (restore func()) {
	old := installEncryptPartitions
	installEncryptPartitions = f
	return func() {
		installEncryptPartitions = old
	}
}

func MockInstallSaveStorageTraits(f func(model gadget.Model, allLaidOutVols map[string]*gadget.Volume, encryptSetupData *install.EncryptionSetupData) error) (restore func()) {
	old := installSaveStorageTraits
	installSaveStorageTraits = f
	return func() {
		installSaveStorageTraits = old
	}
}

func MockMatchDisksToGadgetVolumes(f func(gVols map[string]*gadget.Volume,
	volCompatOpts *gadget.VolumeCompatibilityOptions) (map[string]map[int]*gadget.OnDiskStructure, error)) (restore func()) {
	restore = testutil.Backup(&installMatchDisksToGadgetVolumes)
	installMatchDisksToGadgetVolumes = f
	return restore
}

func MockSecbootStageEncryptionKeyChange(f func(node string, key keys.EncryptionKey) error) (restore func()) {
	restore = testutil.Backup(&secbootStageEncryptionKeyChange)
	secbootStageEncryptionKeyChange = f
	return restore
}

func MockSecbootTransitionEncryptionKeyChange(f func(mountpoint string, key keys.EncryptionKey) error) (restore func()) {
	restore = testutil.Backup(&secbootTransitionEncryptionKeyChange)
	secbootTransitionEncryptionKeyChange = f
	return restore
}

func MockCloudInitStatus(f func() (sysconfig.CloudInitState, error)) (restore func()) {
	old := cloudInitStatus
	cloudInitStatus = f
	return func() {
		cloudInitStatus = old
	}
}

func MockRestrictCloudInit(f func(sysconfig.CloudInitState, *sysconfig.CloudInitRestrictOptions) (sysconfig.CloudInitRestrictionResult, error)) (restore func()) {
	old := restrictCloudInit
	restrictCloudInit = f
	return func() {
		restrictCloudInit = old
	}
}

func DeviceManagerHasFDESetupHook(mgr *DeviceManager, kernelInfo *snap.Info) (bool, error) {
	return mgr.hasFDESetupHook(kernelInfo)
}

func DeviceManagerRunFDESetupHook(mgr *DeviceManager, req *fde.SetupRequest) ([]byte, error) {
	return mgr.runFDESetupHook(req)
}

func DeviceManagerCheckEncryption(mgr *DeviceManager, st *state.State, deviceCtx snapstate.DeviceContext, mode secboot.TPMProvisionMode) (device.EncryptionType, error) {
	return mgr.checkEncryption(st, deviceCtx, mode)
}

func MockTimeutilIsNTPSynchronized(f func() (bool, error)) (restore func()) {
	old := timeutilIsNTPSynchronized
	timeutilIsNTPSynchronized = f
	return func() {
		timeutilIsNTPSynchronized = old
	}
}

func DeviceManagerNTPSyncedOrWaitedLongerThan(mgr *DeviceManager, maxWait time.Duration) bool {
	return mgr.ntpSyncedOrWaitedLongerThan(maxWait)
}

func MockSystemForPreseeding(f func() (string, error)) (restore func()) {
	old := systemForPreseeding
	systemForPreseeding = f
	return func() {
		systemForPreseeding = old
	}
}

func MockSecbootEnsureRecoveryKey(f func(recoveryKeyFile string, rkeyDevs []secboot.RecoveryKeyDevice) (keys.RecoveryKey, error)) (restore func()) {
	restore = testutil.Backup(&secbootEnsureRecoveryKey)
	secbootEnsureRecoveryKey = f
	return restore
}

func MockSecbootRemoveRecoveryKeys(f func(rkeyDevToKey map[secboot.RecoveryKeyDevice]string) error) (restore func()) {
	restore = testutil.Backup(&secbootRemoveRecoveryKeys)
	secbootRemoveRecoveryKeys = f
	return restore
}

func MockSecbootMarkSuccessful(f func() error) (restore func()) {
	r := testutil.Backup(&secbootMarkSuccessful)
	secbootMarkSuccessful = f
	return r
}

func BuildGroundDeviceContext(model *asserts.Model, mode string) snapstate.DeviceContext {
	return &groundDeviceContext{model: model, systemMode: mode}
}

func MockOsutilAddUser(addUser func(name string, opts *osutil.AddUserOptions) error) (restore func()) {
	restore = testutil.Backup(&osutilAddUser)
	osutilAddUser = addUser
	return restore
}

func MockOsutilDelUser(delUser func(name string, opts *osutil.DelUserOptions) error) (restore func()) {
	restore = testutil.Backup(&osutilDelUser)
	osutilDelUser = delUser
	return restore
}

func MockUserLookup(lookup func(username string) (*user.User, error)) (restore func()) {
	restore = testutil.Backup(&userLookup)
	userLookup = lookup
	return restore
}

func EnsureExpiredUsersRemoved(m *DeviceManager) error {
	return m.ensureExpiredUsersRemoved()
}

var ProcessAutoImportAssertions = processAutoImportAssertions

func MockCreateAllKnownSystemUsers(createAllUsers func(state *state.State, assertDb asserts.RODatabase, model *asserts.Model, serial *asserts.Serial, sudoer bool) ([]*CreatedUser, error)) (restore func()) {
	restore = testutil.Backup(&createAllKnownSystemUsers)
	createAllKnownSystemUsers = createAllUsers
	return restore
}

func MockEncryptionSetupDataInCache(st *state.State, label string, recoveryKeyID string, volumesAuth *device.VolumesAuthOptions) (restore func()) {
	st.Lock()
	defer st.Unlock()
	var esd *install.EncryptionSetupData
	labelToEncData := map[string]*install.MockEncryptedDeviceAndRole{
		"ubuntu-save": {
			Role:            "system-save",
			EncryptedDevice: "/dev/mapper/ubuntu-save",
		},
		"ubuntu-data": {
			Role:            "system-data",
			EncryptedDevice: "/dev/mapper/ubuntu-data",
		},
	}
	esd = install.MockEncryptionSetupData(labelToEncData, recoveryKeyID, volumesAuth)
	st.Cache(encryptionSetupDataKey{label}, esd)
	return func() { CleanUpEncryptionSetupDataInCache(st, label) }
}

func GetEncryptionSetupDataFromCache(st *state.State, label string) *install.EncryptionSetupData {
	cached := st.Cached(encryptionSetupDataKey{label})
	if cached == nil {
		return nil
	}
	return cached.(*install.EncryptionSetupData)
}

func CleanUpEncryptionSetupDataInCache(st *state.State, label string) {
	st.Lock()
	defer st.Unlock()
	key := encryptionSetupDataKey{label}
	st.Cache(key, nil)
}

type UniqueSnapsInRecoverySystem = uniqueSnapsInRecoverySystem

func MockSecbootAddBootstrapKeyOnExistingDisk(f func(node string, newKey keys.EncryptionKey) error) (restore func()) {
	old := secbootAddBootstrapKeyOnExistingDisk
	secbootAddBootstrapKeyOnExistingDisk = f
	return func() {
		secbootAddBootstrapKeyOnExistingDisk = old
	}
}

func MockSecbootRenameKeys(f func(node string, renames map[string]string) error) (restore func()) {
	old := secbootRenameKeys
	secbootRenameKeys = f
	return func() {
		secbootRenameKeys = old
	}
}

func MockSecbootCreateBootstrappedContainer(f func(key secboot.DiskUnlockKey, devicePath string) secboot.BootstrappedContainer) (restore func()) {
	old := secbootCreateBootstrappedContainer
	secbootCreateBootstrappedContainer = f
	return func() {
		secbootCreateBootstrappedContainer = old
	}
}

func MockSecbootDeleteKeys(f func(node string, matches map[string]bool) error) (restore func()) {
	old := secbootDeleteKeys
	secbootDeleteKeys = f
	return func() {
		secbootDeleteKeys = old
	}
}

func MockSecbootDeleteOldKeys(f func(devicePath string) error) (restore func()) {
	old := secbootDeleteOldKeys
	secbootDeleteOldKeys = f
	return func() {
		secbootDeleteOldKeys = old
	}
}

func MockSecbootTemporaryNameOldKeys(f func(devicePath string) error) (restore func()) {
	old := secbootTemporaryNameOldKeys
	secbootTemporaryNameOldKeys = f
	return func() {
		secbootTemporaryNameOldKeys = old
	}
}

func MockDisksDMCryptUUIDFromMountPoint(f func(mountpoint string) (string, error)) (restore func()) {
	old := disksDMCryptUUIDFromMountPoint
	disksDMCryptUUIDFromMountPoint = f
	return func() {
		disksDMCryptUUIDFromMountPoint = old
	}
}

func VolumesAuthOptionsKeyByLabel(label string) volumesAuthOptionsKey {
	return volumesAuthOptionsKey{label}
}

func MockSecbootRemoveOldCounterHandles(f func(node string, possibleOldKeys map[string]bool, possibleKeyFiles []string, hintExpectFDEHook bool) error) (restore func()) {
	old := secbootRemoveOldCounterHandles
	secbootRemoveOldCounterHandles = f
	return func() {
		secbootRemoveOldCounterHandles = old
	}
}

func MockFdestateGenerateRecoveryKey(f func(st *state.State) (rkey keys.RecoveryKey, keyID string, err error)) (restore func()) {
	return testutil.Mock(&fdestateGenerateRecoveryKey, f)
}

func MockFdestateGetRecoveryKey(f func(st *state.State, keyID string) (rkey keys.RecoveryKey, err error)) (restore func()) {
	return testutil.Mock(&fdestateGetRecoveryKey, f)
}

func MockFdestateGetKeyslots(f func(st *state.State, keyslotRefs []fdestate.KeyslotRef) (keyslots []fdestate.Keyslot, missingRefs []fdestate.KeyslotRef, err error)) (restore func()) {
	return testutil.Mock(&fdestateGetKeyslots, f)
}

func MockSnapstateGadgetInfo(f func(st *state.State, deviceCtx snapstate.DeviceContext) (*snap.Info, error)) (restore func()) {
	return testutil.Mock(&snapstateGadgetInfo, f)
}
