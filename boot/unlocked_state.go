package boot

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
)

// states for partition state
const (
	// states for MountState
	PartitionMounted          = "mounted"
	PartitionErrMounting      = "error-mounting"
	PartitionAbsentOptional   = "absent-but-optional"
	PartitionMountedUntrusted = "mounted-untrusted"
	// states for UnlockState
	PartitionUnlocked     = "unlocked"
	PartitionErrUnlocking = "error-unlocking"
	// keys used to unlock for UnlockKey
	KeyRun      = "run"
	KeyFallback = "fallback"
	KeyRecovery = "recovery"

	// Name for the unlock state file
	UnlockedStateFileName = "unlocked.json"
	// Legacy name for the unlock state file for degraded cases
	DegradedStateFileName = "degraded.json"
)

// PartitionState is the state of a partition after recover mode has completed
// for degraded mode.
type PartitionState struct {
	// MountState is whether the partition was mounted successfully or not.
	// This state is not provided in run mode.
	MountState string `json:"mount-state,omitempty"`
	// MountLocation is where the partition was mounted.
	// This state is not provided in run mode.
	MountLocation string `json:"mount-location,omitempty"`
	// UnlockState is whether the partition was unlocked successfully or not.
	UnlockState string `json:"unlock-state,omitempty"`
	// UnlockKey is what key the partition was unlocked with, either "run",
	// "fallback" or "recovery".
	UnlockKey string `json:"unlock-key,omitempty"`
}

// DiskUnlockState represents the unlocking state of all encrypted
// containers
type DiskUnlockState struct {
	// UbuntuData is the state of the ubuntu-data (or ubuntu-data-enc)
	// partition.
	UbuntuData PartitionState `json:"ubuntu-data,omitempty"`
	// UbuntuBoot is the state of the ubuntu-boot partition.
	UbuntuBoot PartitionState `json:"ubuntu-boot,omitempty"`
	// UbuntuSave is the state of the ubuntu-save (or ubuntu-save-enc)
	// partition.
	UbuntuSave PartitionState `json:"ubuntu-save,omitempty"`
}

// WriteTo writes the DiskUnlockState into a json file for given name
// in the snap-bootstrap /run dir.
func (r *DiskUnlockState) WriteTo(name string) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dirs.SnapBootstrapRunDir, 0755); err != nil {
		return err
	}

	// leave the information about degraded state at an ephemeral location
	return os.WriteFile(filepath.Join(dirs.SnapBootstrapRunDir, name), b, 0644)
}

// LoadDiskUnlockState reads the DiskUnlockState from a json file for
// given name in the snap-bootstrap /run dir.
func LoadDiskUnlockState(name string) (*DiskUnlockState, error) {
	jsonFile := filepath.Join(dirs.SnapBootstrapRunDir, name)
	b, err := os.ReadFile(jsonFile)
	if err != nil {
		return nil, err
	}

	ret := &DiskUnlockState{}
	err = json.Unmarshal(b, &ret)
	if err != nil {
		return nil, err
	}

	return ret, nil
}

// IsUnlockedWithRecoveryKey tells whether a recovery key has been
// typed to unlock a disk during boot.
func IsUnlockedWithRecoveryKey() (bool, error) {
	state, err := LoadDiskUnlockState(UnlockedStateFileName)
	if err != nil {
		return false, err
	}

	return state.UbuntuData.UnlockKey == "recovery" || state.UbuntuSave.UnlockKey == "recovery", nil
}
