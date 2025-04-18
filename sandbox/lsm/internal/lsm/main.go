package main

import (
	"encoding/base64"
	"fmt"
	"os"

	"github.com/snapcore/snapd/sandbox/lsm"
)

var (
	lsmIDs = map[uint64]string{
		lsm.LSM_ID_CAPABILITY: "capability",
		lsm.LSM_ID_SELINUX:    "selinux",
		lsm.LSM_ID_SMACK:      "smack",
		lsm.LSM_ID_TOMOYO:     "tomoyo",
		lsm.LSM_ID_APPARMOR:   "apparmor",
		lsm.LSM_ID_YAMA:       "yama",
		lsm.LSM_ID_LOADPIN:    "loadpin",
		lsm.LSM_ID_SAFESETID:  "safesetid",
		lsm.LSM_ID_LOCKDOWN:   "lockdown",
		lsm.LSM_ID_BPF:        "bpf",
		lsm.LSM_ID_LANDLOCK:   "landlock",
		lsm.LSM_ID_IMA:        "ima",
		lsm.LSM_ID_EVM:        "evm",
		lsm.LSM_ID_IPE:        "ipe",
	}

	lsmWithStringContetx = map[uint64]bool{
		lsm.LSM_ID_SELINUX:  true,
		lsm.LSM_ID_APPARMOR: true,
	}
)

func lsmIDToName(id uint64) string {
	name := lsmIDs[id]
	if name == "" && id != lsm.LSM_ID_UNDEF {
		// not LSM_ID_UNDEF, could be a new LSM we don't know yet
		name = fmt.Sprintf("(lsm-id:%v)", id)
	}
	return name
}

func run() error {
	lsms, err := lsm.List()
	if err != nil {
		return err
	}

	fmt.Printf("found %v active LSMs\n", len(lsms))
	for _, id := range lsms {
		fmt.Printf("- (%4d) %s\n", id, lsmIDToName(id))
	}

	entries, err := lsm.CurrentContext()
	if err != nil {
		return err
	}

	fmt.Printf("context entries: %v\n", len(entries))

	for _, e := range entries {
		currentName := lsmIDToName(e.ID)
		if lsmWithStringContetx[e.ID] {
			fmt.Printf("current %v LSM context: %v\n", currentName, string(e.Context))
		} else {
			fmt.Printf("current %v LSM context (binary): %v\n", currentName, base64.StdEncoding.EncodeToString(e.Context))
		}
	}

	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "err: %v\n", err)
		os.Exit(1)
	}
}
