package boot

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
)

// states for partition state
const (
	// states for LocateState
	PartitionFound      = "found"
	PartitionNotFound   = "not-found"
	PartitionErrFinding = "error-finding"
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
)

// PartitionState is the state of a partition after recover mode has completed
// for degraded mode.
type PartitionState struct {
	// MountState is whether the partition was mounted successfully or not.
	MountState string `json:"mount-state,omitempty"`
	// MountLocation is where the partition was mounted.
	MountLocation string `json:"mount-location,omitempty"`
	// Device is what device the partition corresponds to. It can be the
	// physical block device if the partition is unencrypted or if it was not
	// successfully unlocked, or it can be a decrypted mapper device if the
	// partition was encrypted and successfully decrypted, or it can be the
	// empty string (or missing) if the partition was not found at all.
	Device string `json:"device,omitempty"`
	// FindState indicates whether the partition was found on the disk or not.
	FindState string `json:"find-state,omitempty"`
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
	// ErrorLog is the log of error messages encountered during recover mode
	// setting up degraded mode.
	ErrorLog []string `json:"error-log"`
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
