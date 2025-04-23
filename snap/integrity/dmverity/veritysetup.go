// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package dmverity

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unsafe"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
)

const (
	// DefaultVerityFormat corresponds to veritysetup's default option for the --format argument which
	// currently is 1. This corresponds to the hash_type field of a dm-verity superblock.
	DefaultVerityFormat = 1
	// DefaultSuperblockVersion corresponds to the superblock version. Version 1 is the only one
	// currently supported by veritysetup. This corresponds to the version field of a dm-verity superblock.
	DefaultSuperblockVersion = 1
)

func getVal(line string) (string, error) {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return "", errors.New("internal error: unexpected veritysetup output format")
	}
	return strings.TrimSpace(parts[1]), nil
}

func getFieldFromOutput(output []byte, key string) (val string, err error) {
	scanner := bufio.NewScanner(bytes.NewBuffer(output))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, key) {
			val, err = getVal(line)
			if err != nil {
				return "", err
			}
		}
	}

	if err = scanner.Err(); err != nil {
		return "", err
	}

	return val, nil
}

func getRootHashFromOutput(output []byte) (rootHash string, err error) {
	rootHash, err = getFieldFromOutput(output, "Root hash")
	if err != nil {
		return "", err
	}
	if len(rootHash) != 64 {
		return "", errors.New("internal error: unexpected root hash length")
	}

	hashAlg, err := getFieldFromOutput(output, "Hash algorithm")
	if err != nil {
		return "", err
	}
	if hashAlg != "sha256" {
		return "", errors.New("internal error: unexpected hash algorithm")
	}

	return rootHash, nil
}

func verityVersion() (major, minor, patch int, err error) {
	output, stderr, err := osutil.RunSplitOutput("veritysetup", "--version")
	if err != nil {
		return -1, -1, -1, osutil.OutputErrCombine(output, stderr, err)
	}

	exp := regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)
	match := exp.FindStringSubmatch(string(output))
	if len(match) != 4 {
		return -1, -1, -1, fmt.Errorf("cannot detect veritysetup version from: %s", string(output))
	}
	major, err = strconv.Atoi(match[1])
	if err != nil {
		return -1, -1, -1, fmt.Errorf("cannot detect veritysetup version from: %s", string(output))
	}
	minor, err = strconv.Atoi(match[2])
	if err != nil {
		return -1, -1, -1, fmt.Errorf("cannot detect veritysetup version from: %s", string(output))
	}
	patch, err = strconv.Atoi(match[3])
	if err != nil {
		return -1, -1, -1, fmt.Errorf("cannot detect veritysetup version from: %s", string(output))
	}
	return major, minor, patch, nil
}

func shouldApplyNewFileWorkaroundForOlderThan204() (bool, error) {
	major, minor, patch, err := verityVersion()
	if err != nil {
		return false, err
	}

	// From version 2.0.4 we don't need this anymore
	if major > 2 || (major == 2 && (minor > 0 || patch >= 4)) {
		return false, nil
	}
	return true, nil
}

// DmVerityParams contains the options to veritysetup format.
type DmVerityParams struct {
	Format        uint8  `json:"format"`
	Hash          string `json:"hash"`
	DataBlocks    uint64 `json:"data-blocks"`
	DataBlockSize uint64 `json:"data-block-size"`
	HashBlockSize uint64 `json:"hash-block-size"`
	Salt          string `json:"salt"`
}

// appendArguments returns the options to veritysetup format as command line arguments.
func (p DmVerityParams) appendArguments(args []string) []string {

	args = append(args, fmt.Sprintf("--format=%d", p.Format))
	args = append(args, fmt.Sprintf("--hash=%s", p.Hash))
	args = append(args, fmt.Sprintf("--data-blocks=%d", p.DataBlocks))
	args = append(args, fmt.Sprintf("--data-block-size=%d", p.DataBlockSize))
	args = append(args, fmt.Sprintf("--hash-block-size=%d", p.HashBlockSize))

	if len(p.Salt) != 0 {
		args = append(args, fmt.Sprintf("--salt=%s", p.Salt))
	}

	return args
}

// Format runs "veritysetup format" with the passed parameters and returns the dm-verity root hash.
//
// "veritysetup format" calculates the hash verification data for dataDevice and stores them in
// hashDevice including the dm-verity superblock. The root hash is retrieved from the command's stdout.
func Format(dataDevice string, hashDevice string, opts *DmVerityParams) (rootHash string, err error) {
	// In older versions of cryptsetup there is a bug when cryptsetup writes
	// its superblock header, and there isn't already preallocated space.
	// Fixed in commit dc852a100f8e640dfdf4f6aeb86e129100653673 which is version 2.0.4
	deploy, err := shouldApplyNewFileWorkaroundForOlderThan204()
	if err != nil {
		return "", err
	} else if deploy {
		space := make([]byte, 4096)
		os.WriteFile(hashDevice, space, 0644)
	}

	args := []string{
		"format",
		dataDevice,
		hashDevice,
	}

	if opts != nil {
		args = opts.appendArguments(args)
	}

	output, stderr, err := osutil.RunSplitOutput("veritysetup", args...)
	if err != nil {
		return "", osutil.OutputErrCombine(output, stderr, err)
	}

	logger.Debugf("cmd: 'veritysetup format %s %s %s':\n%s", dataDevice, hashDevice, args, osutil.CombineStdOutErr(output, stderr))

	rootHash, err = getRootHashFromOutput(output)
	if err != nil {
		return "", err
	}

	return rootHash, nil
}

// VeritySuperblock represents the dm-verity superblock structure.
//
// It mirrors cryptsetup's verity_sb structure from
// https://gitlab.com/cryptsetup/cryptsetup/-/blob/main/lib/verity/verity.c?ref_type=heads#L25
type VeritySuperblock struct {
	Signature     [8]uint8  /* "verity\0\0" */
	Version       uint32    /* superblock version */
	HashType      uint32    /* 0 - Chrome OS, 1 - normal */
	Uuid          [16]uint8 /* UUID of hash device */
	Algorithm     [32]uint8 /* hash algorithm name */
	DataBlockSize uint32    /* data block in bytes */
	HashBlockSize uint32    /* hash block in bytes */
	DataBlocks    uint64    /* number of data blocks */
	SaltSize      uint16    /* salt size */
	Pad1          [6]uint8
	Salt          [256]uint8 /* salt */
	Pad2          [168]uint8
}

func (sb *VeritySuperblock) Size() int {
	size := int(unsafe.Sizeof(*sb))
	return size
}

// Validate will perform consistency checks over an extracted superblock to determine whether it's a valid
// superblock or not.
func (sb *VeritySuperblock) validate() error {
	if sb.Version != DefaultSuperblockVersion {
		return errors.New("invalid dm-verity superblock version")
	}

	if sb.HashType != DefaultVerityFormat {
		return errors.New("invalid dm-verity hash type")
	}

	return nil
}

func (sb *VeritySuperblock) EncodedSalt() string {
	return hex.EncodeToString(sb.Salt[:sb.SaltSize])
}

// ReadSuperblock reads the dm-verity superblock from a dm-verity hash file.
func ReadSuperblock(filename string) (*VeritySuperblock, error) {
	hashFile, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer hashFile.Close()
	var sb VeritySuperblock
	verity_sb := make([]byte, sb.Size())
	if _, err := hashFile.Read(verity_sb); err != nil {
		return nil, err
	}
	err = binary.Read(bytes.NewReader(verity_sb), binary.LittleEndian, &sb)
	if err != nil {
		return nil, err
	}

	err = sb.validate()
	if err != nil {
		return nil, err
	}

	return &sb, nil
}
