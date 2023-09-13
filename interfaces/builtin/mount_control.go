// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package builtin

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/utils"
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/systemd"
)

const mountControlSummary = `allows creating transient and persistent mounts`

const mountControlBaseDeclarationPlugs = `
  mount-control:
    allow-installation: false
    deny-auto-connection: true
`

const mountControlBaseDeclarationSlots = `
  mount-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-connection: true
`

var mountAttrTypeError = errors.New(`mount-control "mount" attribute must be a list of dictionaries`)

const mountControlConnectedPlugSecComp = `
# Description: Allow mount and umount syscall access. No filtering here, as we
# rely on AppArmor to filter the mount operations.
mount
umount
umount2
`

// The reason why this list is not shared with osutil.MountOptsToCommonFlags or
// other parts of the codebase is that this one only contains the options which
// have been deemed safe and have been vetted by the security team.
var allowedKernelMountOptions = []string{
	"async",
	"atime",
	"bind",
	"diratime",
	"dirsync",
	"iversion",
	"lazytime",
	"noatime",
	"nodev",
	"nodiratime",
	"noexec",
	"noiversion",
	"nolazytime",
	"nomand",
	"norelatime",
	"nostrictatime",
	"nosuid",
	"nouser",
	"relatime",
	"ro",
	"rw",
	"strictatime",
	"sync",
}

// These mount options are evaluated by mount(8) only and never reach the kernel
var allowedUserspaceMountOptions = []string{
	"auto",
	"defaults",
	"noauto",
	"nofail",
	"nogroup",
	"noowner",
	"nousers",
}

// This map was obtained by referencing the mount(8) manpage, the manpages for
// individual filesystems, and examining filesystem source code in the fs/
// directory of the linux kernel source, in that order.
var allowedFilesystemSpecificMountOptions = map[string][]string{
	"adfs":       {"uid=", "gid=", "ownmask=", "othmask="},
	"affs":       {"uid=", "gid=", "setuid=", "setgid=", "mode=", "protect", "usemp", "verbose", "prefix=", "volume=", "reserved=", "root=", "bs=", "grpquota", "noquota", "quota", "usrquota"},
	"aufs":       {"br:", "dirs=", "add:", "ins:", "del:", "mod:", "append:", "prepend:", "xino=", "noxino", "trunc_xib", "notrunk_xib", "create_policy=", "create=", "copyup_policy=", "copyup=", "cpup=", "verbose", "v", "noverbose", "quiet", "q", "silent", "sum", "nosum", "dirwh=", "plink", "noplink", "clean_plink", "udba=", "diropq=", "warn_perm", "nowarm_perm", "coo=", "dlgt", "nodlgt", "shwh", "noshwh"},
	"autofs":     {}, // autofs mount options are specified via entries in an autofs map
	"btrfs":      {"acl", "noacl", "autodefrag", "noautodefrag", "barrier", "nobarrier", "check_int", "check_int_data", "check_int_print_mask=", "clear_cache", "commit=", "compress", "compress=", "compress-force", "compress-force=", "datacow", "nodatacow", "datasum", "nodatasum", "degraded", "device=", "discard", "discard=", "nodiscard", "enospc_debug", "noenospc_debug", "fatal_errors=", "flushoncommit", "noflushoncommit", "fragment=", "nologreplay", "max_inline=", "metadata_ratio=", "norecovery", "rescan_uuid_tree", "rescue", "skip_balance", "space_cache", "space_cache=", "nospace_cache", "ssd", "ssd_spread", "nossd", "nossd_spread", "subvol=", "subvolid=", "thread_pool=", "treelog", "notreelog", "usebackuproot", "user_subvol_rm_allowed", "recovery", "inode_cache", "noinode_cache"},
	"cifs":       {"username=", "user=", "password=", "pass=", "credentials=", "cred=", "uid=", "forceuid", "cruid=", "gid=", "forcegid", "idsfromsid", "port=", "netbiosname=", "servern=", "file_mode=", "dir_mode=", "ip=", "addr=", "domain=", "dom=", "workgroup=", "domainauto", "guest", "iocharset", "setuids", "nosetuids", "perm", "noperm", "denperm", "cache=", "handlecache", "nohandlecache", "handletimeout", "rwpidforward", "mapchars", "nomapchars", "mapposix", "intr", "nointr", "hard", "soft", "noacl", "cifsacl", "backupuid=", "backupgid=", "nocase", "ignorecase", "sec=", "seal", "rdma", "resilienthandles", "noresilienthandles", "persistenthandles", "nopersistenthandles", "snapshot=", "nobrl", "forcemandatorylock", "locallease", "nolease", "sfu", "mfsymlinks", "echo_interval=", "serverino", "noserverino", "posix", "unix", "linux", "noposix", "nounix", "nolinux", "nouser_xattr", "nodfs", "noautotune", "nosharesock", "noblocksend", "rsize=", "wsize=", "bsize=", "max_credits=", "fsc", "multiuser", "actimeo=", "noposixpaths", "posixpaths", "vers="},
	"debugfs":    {"uid=", "gid=", "mode="},                             // debugfs not allowed by mount-control but listed for completeness
	"devpts":     {"uid=", "gid=", "mode=", "newinstance", "ptmxmode="}, // devpts not allowed by mount-control but listed for completeness
	"ext2":       {"acl", "noacl", "bsddf", "minixdf", "check=", "nocheck", "debug", "errors=", "grpid", "bsdgroups", "nogrpid", "sysvgroups", "grpquota", "noquota", "quota", "usrquota", "nouid32", "oldalloc", "orlov", "resgid=", "resuid=", "sb=", "user_xattr", "nouser_xattr"},
	"ext3":       {"acl", "noacl", "bsddf", "minixdf", "check=", "nocheck", "debug", "errors=", "grpid", "bsdgroups", "nogrpid", "sysvgroups", "grpquota", "noquota", "quota", "usrquota", "nouid32", "oldalloc", "orlov", "resgid=", "resuid=", "sb=", "user_xattr", "nouser_xattr", "journal_dev=", "journal_path=", "norecovery", "noload", "data=", "data_err=", "barrier=", "commit=", "user_xattr", "jqfmt=", "usrjquota=", "grpjquota="},
	"ext4":       {"journal_dev=", "journal_path=", "norecovery", "noload", "data=", "commit=", "orlov", "oldalloc", "user_xattr", "nouser_xattr", "acl", "noacl", "bsddf", "minixdf", "debug", "errors=", "data_err=", "grpid", "bsdgroups", "nogrpid", "sysvgroups", "resgid=", "resuid=", "sb=", "quota", "noquota", "nouid32", "grpquota", "usrquota", "usrjquota=", "grpjquota=", "jqfmt=", "journal_checksum", "nojournal_checksum", "journal_async_commit", "barrier=", "inode_readahead_blks=", "stripe=", "delalloc", "nodelalloc", "max_batch_time=", "min_batch_time=", "journal_ioprio=", "abort", "auto_da_alloc", "noauto_da_alloc", "noinit_itable", "init_itable=", "discard", "nodiscard", "block_validity", "noblock_validity", "dioread_lock", "dioread_nolock", "max_dir_size_kb=", "i_version", "nombcache", "prjquota"},
	"fat":        {"blocksize=", "uid=", "gid=", "umask=", "dmask=", "fmask=", "allow_utime=", "check=", "codepage=", "conv=", "cvf_format=", "cvf_option", "debug", "discard", "dos1xfloppy", "errors=", "fat=", "iocharset=", "nfs=", "tz=", "time_offset=", "quiet", "rodir", "showexec", "sys_immutable", "flush", "usefree", "dots", "nodots", "dotsOK="},
	"functionfs": {"no_disconnect=", "rmode=", "fmode=", "mode=", "uid=", "gid="},
	"fuse":       {"default_permissions", "allow_other", "rootmode=", "blkdev", "blksize=", "max_read=", "fd=", "user_id=", "fsname=", "subtype=", "allow_root", "auto_unmount", "kernel_cache", "auto_cache", "umask=", "uid=", "gid=", "entry_timeout=", "negative_timeout=", "attr_timeout=", "ac_attr_timeout=", "noforget", "remember=", "modules=", "setuid=", "drop_privileges"},
	"hfs":        {"creator=", "type=", "uid=", "gid=", "dir_umask=", "file_umask=", "umask=", "session=", "part=", "quiet"},
	"hpfs":       {"uid=", "gid=", "umask=", "case=", "conv=", "nocheck"},
	"iso9660":    {"norock", "nojoliet", "check=", "uid=", "gid=", "map=", "mode=", "unhide", "block=", "conv=", "cruft", "session=", "sbsector=", "iocharset=", "utf8"},
	"jfs":        {"iocharset=", "resize=", "nointegrity", "integrity", "errors=", "noquota", "quota", "usrquota", "grpquota"},
	"msdos":      {"blocksize=", "uid=", "gid=", "umask=", "dmask=", "fmask=", "allow_utime=", "check=", "codepage=", "conv=", "cvf_format=", "cvf_option", "debug", "discard", "dos1xfloppy", "errors=", "fat=", "iocharset=", "nfs=", "tz=", "time_offset=", "quiet", "rodir", "showexec", "sys_immutable", "flush", "usefree", "dots", "nodots", "dotsOK="},
	"nfs":        {"nfsvers=", "vers=", "soft", "hard", "softreval", "nosoftreval", "intr", "nointr", "timeo=", "retrans=", "rsize=", "wsize=", "ac", "noac", "acregmin=", "acregmax=", "acdirmin=", "acdirmax=", "actimeo=", "bg", "fg", "nconnect=", "max_connect=", "rdirplus", "nordirplus", "retry=", "sec=", "sharecache", "nosharecache", "revsport", "norevsport", "lookupcache=", "fsc", "nofsc", "sloppy", "proto=", "udp", "tcp", "rdma", "port=", "mountport=", "mountproto=", "mounthost=", "mountvers=", "namlen=", "lock", "nolock", "cto", "nocto", "acl", "noacl", "local_lock=", "minorversion=", "clientaddr=", "migration", "nomigration"},
	"nfs4":       {"nfsvers=", "vers=", "soft", "hard", "softreval", "nosoftreval", "intr", "nointr", "timeo=", "retrans=", "rsize=", "wsize=", "ac", "noac", "acregmin=", "acregmax=", "acdirmin=", "acdirmax=", "actimeo=", "bg", "fg", "nconnect=", "max_connect=", "rdirplus", "nordirplus", "retry=", "sec=", "sharecache", "nosharecache", "revsport", "norevsport", "lookupcache=", "fsc", "nofsc", "sloppy", "proto=", "minorversion=", "port=", "cto", "nocto", "clientaddr=", "migration", "nomigration"},
	"ntfs":       {"iocharset=", "nls=", "utf8", "uni_xlate=", "posix=", "uid=", "gid=", "umask="},
	"ntfs-3g":    {"acl", "allow_other", "big_writes", "compression", "debug", "delay_mtime", "delay_mtime=", "dmask=", "efs_raw", "fmask=", "force", "hide_dot_files", "hide_hid_files", "inherit", "locale=", "max_read=", "no_def_opts", "no_detach", "nocompression", "norecover", "permissions", "posix_nlink", "recover", "remove_hiberfile", "show_sys_files", "silent", "special_files=", "streams_interface=", "uid=", "gid=", "umask=", "usermapping=", "user_xattr", "windows_names"},
	"lowntfs-3g": {"acl", "allow_other", "big_writes", "compression", "debug", "delay_mtime", "delay_mtime=", "dmask=", "efs_raw", "fmask=", "force", "hide_dot_files", "hide_hid_files", "ignore_case", "inherit", "locale=", "max_read=", "no_def_opts", "no_detach", "nocompression", "norecover", "permissions", "posix_nlink", "recover", "remove_hiberfile", "show_sys_files", "silent", "special_files=", "streams_interface=", "uid=", "gid=", "umask=", "usermapping=", "user_xattr", "windows_names"},
	"overlay":    {"lowerdir=", "upperdir=", "workdir=", "userxattr", "redirect_dir=", "index=", "uuid=", "nfs_export=", "xino=", "metacopy=", "volatile"}, // overlayfs not allowed by mount-control but listed for completeness
	"ramfs":      {},
	"reiserfs":   {"conv", "hash=", "hashed_relocation", "no_unhashed_relocation", "noborder", "nolog", "notail", "replayonly", "resize=", "user_xattr", "acl", "barrier="},
	"squashfs":   {},
	"tmpfs":      {"size=", "nr_blocks=", "nr_inodes=", "mode=", "gid=", "uid=", "huge=", "mpol="},
	"ubifs":      {"bulk_read", "no_bulk_read", "chk_data_crc", "no_chk_data_crc", "compr="},
	"udf":        {"uid=", "gid=", "umask=", "mode=", "dmode=", "bs=", "unhide", "undelete", "adinicb", "noadinicb", "shortad", "longad", "nostrict", "iocharset=", "utf8", "novrs", "session=", "anchor=", "lastblock="},
	"ufs":        {"ufstype=", "onerror="},
	"umsdos":     {"blocksize=", "uid=", "gid=", "umask=", "dmask=", "fmask=", "allow_utime=", "check=", "codepage=", "conv=", "cvf_format=", "cvf_option", "debug", "discard", "dos1xfloppy", "errors=", "fat=", "iocharset=", "nfs=", "tz=", "time_offset=", "quiet", "rodir", "showexec", "sys_immutable", "flush", "usefree", "dots", "nodots"},
	"vfat":       {"blocksize=", "uid=", "gid=", "umask=", "dmask=", "fmask=", "allow_utime=", "check=", "codepage=", "conv=", "cvf_format=", "cvf_option", "debug", "discard", "dos1xfloppy", "errors=", "fat=", "iocharset=", "nfs=", "tz=", "time_offset=", "quiet", "rodir", "showexec", "sys_immutable", "flush", "usefree", "dots", "uni_xlate", "posix", "nonumtail", "utf8", "shortname="},
	"usbfs":      {"devuid=", "devgid=", "devmode=", "busiud=", "busgid=", "busmode=", "listuid=", "listgid=", "listmode="},
	"xfs":        {"allocsize=", "attr2", "noattr2", "dax=", "discard", "nodiscard", "grpid", "bsdgroups", "nogrpid", "sysvgroups", "filestreams", "ikeep", "noikeep", "inode32", "inode64", "largeio", "nolargeio", "logbufs=", "logbsize=", "logdev=", "rtdev=", "noalign", "norecovery", "nouuid", "noquota", "unquota", "usrquota", "quota", "uqnoenforce", "qnoenforce", "gquota", "grpquota", "gqnoenforce", "pquota", "prjquota", "pqnoenforce", "sunit=", "swidth=", "sqalloc", "wsync"},
	"zfs":        {"context=", "fscontext=", "defcontext=", "rootcontext=", "xattr", "noxattr"},
}

var filesystemsWithColonSeparatedOptions = []string{
	"aufs",
}

// A few mount flags are special in that if they are specified, the filesystem
// type is ignored. We list them here, and we will ensure that the plug
// declaration does not specify a type, if any of them is present among the
// options.
var optionsWithoutFsType = []string{
	"bind",
	// Note: the following flags should fall into this list, but we are
	// not currently allowing them (and don't plan to):
	// - "make-private"
	// - "make-rprivate"
	// - "make-rshared"
	// - "make-rslave"
	// - "make-runbindable"
	// - "make-shared"
	// - "make-slave"
	// - "make-unbindable"
	// - "move"
	// - "rbind"
	// - "remount"
}

// List of filesystem types to allow if the plug declaration does not
// explicitly specify a filesystem type.
var defaultFSTypes = []string{
	"aufs",
	"autofs",
	"btrfs",
	"ext2",
	"ext3",
	"ext4",
	"hfs",
	"iso9660",
	"jfs",
	"msdos",
	"ntfs",
	"ramfs",
	"reiserfs",
	"squashfs",
	"tmpfs",
	"ubifs",
	"udf",
	"ufs",
	"vfat",
	"xfs",
	"zfs",
}

// The filesystems in the following list were considered either dangerous or
// not relevant for this interface:
var disallowedFSTypes = []string{
	"bpf",
	"cgroup",
	"cgroup2",
	"debugfs",
	"devpts",
	"ecryptfs",
	"hugetlbfs",
	"overlayfs",
	"proc",
	"securityfs",
	"sysfs",
	"tracefs",
}

// mountControlInterface allows creating transient and persistent mounts
type mountControlInterface struct {
	commonInterface
}

// The "what" and "where" attributes end up in the AppArmor profile, surrounded
// by double quotes; to ensure that a malicious snap cannot inject arbitrary
// rules by specifying something like
//
//	where: $SNAP_DATA/foo", /** rw, #
//
// which would generate a profile line like:
//
//	mount options=() "$SNAP_DATA/foo", /** rw, #"
//
// (which would grant read-write access to the whole filesystem), it's enough
// to exclude the `"` character: without it, whatever is written in the
// attribute will not be able to escape being treated like a pattern.
//
// To be safe, there's more to be done: the pattern also needs to be valid, as
// a malformed one (for example, a pattern having an unmatched `}`) would cause
// apparmor_parser to fail loading the profile. For this situation, we use the
// PathPattern interface to validate the pattern.
//
// Besides that, we are also excluding the `@` character, which is used to mark
// AppArmor variables (tunables): when generating the profile we lack the
// knowledge of which variables have been defined, so it's safer to exclude
// them.
// The what attribute regular expression here is intentionally permissive of
// nearly any path, and due to the super-privileged nature of this interface it
// is expected that sensible values of what are enforced by the store manual
// review queue and security teams.
var (
	whatRegexp  = regexp.MustCompile(`^(none|/[^"@]*)$`)
	whereRegexp = regexp.MustCompile(`^(\$SNAP_COMMON|\$SNAP_DATA)?/[^\$"@]+$`)
)

// Excluding spaces and other characters which might allow constructing a
// malicious string like
//
//	auto) options=() /malicious/content /var/lib/snapd/hostfs/...,\n mount fstype=(
var typeRegexp = regexp.MustCompile(`^[a-z0-9]+$`)

type MountInfo struct {
	what       string
	where      string
	persistent bool
	types      []string
	options    []string
}

func (mi *MountInfo) isType(typ string) bool {
	return len(mi.types) == 1 && mi.types[0] == typ
}

func (mi *MountInfo) hasType() bool {
	return len(mi.types) > 0
}

func parseStringList(mountEntry map[string]interface{}, fieldName string) ([]string, error) {
	var list []string
	value, ok := mountEntry[fieldName]
	if ok {
		interfaceList, ok := value.([]interface{})
		if !ok {
			return nil, fmt.Errorf(`mount-control "%s" must be an array of strings (got %q)`, fieldName, value)
		}
		for i, iface := range interfaceList {
			valueString, ok := iface.(string)
			if !ok {
				return nil, fmt.Errorf(`mount-control "%s" element %d not a string (%q)`, fieldName, i+1, iface)
			}
			list = append(list, valueString)
		}
	}
	return list, nil
}

func enumerateMounts(plug interfaces.Attrer, fn func(mountInfo *MountInfo) error) error {
	var mounts []map[string]interface{}
	err := plug.Attr("mount", &mounts)
	if err != nil && !errors.Is(err, snap.AttributeNotFoundError{}) {
		return mountAttrTypeError
	}

	for _, mount := range mounts {
		what, ok := mount["what"].(string)
		if !ok {
			return fmt.Errorf(`mount-control "what" must be a string`)
		}

		where, ok := mount["where"].(string)
		if !ok {
			return fmt.Errorf(`mount-control "where" must be a string`)
		}

		persistent := false
		persistentValue, ok := mount["persistent"]
		if ok {
			if persistent, ok = persistentValue.(bool); !ok {
				return fmt.Errorf(`mount-control "persistent" must be a boolean`)
			}
		}

		types, err := parseStringList(mount, "type")
		if err != nil {
			return err
		}

		options, err := parseStringList(mount, "options")
		if err != nil {
			return err
		}

		mountInfo := &MountInfo{
			what:       what,
			where:      where,
			persistent: persistent,
			types:      types,
			options:    options,
		}

		if err := fn(mountInfo); err != nil {
			return err
		}
	}

	return nil
}

func validateNoAppArmorRegexpWithError(errPrefix string, strList ...string) error {
	for _, str := range strList {
		if err := apparmor_sandbox.ValidateNoAppArmorRegexp(str); err != nil {
			return fmt.Errorf(errPrefix+`: %w`, err)
		}
	}
	return nil
}

func validateWhatAttr(mountInfo *MountInfo) error {
	what := mountInfo.what

	// with "functionfs" the "what" can essentially be anything, see
	// https://www.kernel.org/doc/html/latest/usb/functionfs.html
	if mountInfo.isType("functionfs") {
		return validateNoAppArmorRegexpWithError(`cannot use mount-control "what" attribute`, what)
	}

	if !whatRegexp.MatchString(what) {
		return fmt.Errorf(`mount-control "what" attribute is invalid: must start with / and not contain special characters`)
	}

	if !cleanSubPath(what) {
		return fmt.Errorf(`mount-control "what" pattern is not clean: %q`, what)
	}

	const allowCommas = true
	if _, err := utils.NewPathPattern(what, allowCommas); err != nil {
		return fmt.Errorf(`mount-control "what" setting cannot be used: %v`, err)
	}

	// "what" must be set to "none" iff the type is "tmpfs"
	isTmpfs := mountInfo.isType("tmpfs")
	if mountInfo.what == "none" {
		if !isTmpfs {
			return errors.New(`mount-control "what" attribute can be "none" only with "tmpfs"`)
		}
	} else if isTmpfs {
		return fmt.Errorf(`mount-control "what" attribute must be "none" with "tmpfs"; found %q instead`, mountInfo.what)
	}

	return nil
}

func validateWhereAttr(where string) error {
	if !whereRegexp.MatchString(where) {
		return fmt.Errorf(`mount-control "where" attribute must start with $SNAP_COMMON, $SNAP_DATA or / and not contain special characters`)
	}

	if !cleanSubPath(where) {
		return fmt.Errorf(`mount-control "where" pattern is not clean: %q`, where)
	}

	const allowCommas = true
	if _, err := utils.NewPathPattern(where, allowCommas); err != nil {
		return fmt.Errorf(`mount-control "where" setting cannot be used: %v`, err)
	}

	return nil
}

func validateMountTypes(types []string) error {
	includesTmpfs := false
	for _, t := range types {
		if !typeRegexp.MatchString(t) {
			return fmt.Errorf(`mount-control filesystem type invalid: %q`, t)
		}
		if strutil.ListContains(disallowedFSTypes, t) {
			return fmt.Errorf(`mount-control forbidden filesystem type: %q`, t)
		}
		if t == "tmpfs" {
			includesTmpfs = true
		}
	}

	if includesTmpfs && len(types) > 1 {
		return errors.New(`mount-control filesystem type "tmpfs" cannot be listed with other types`)
	}
	return nil
}

func validateMountOptions(mountInfo *MountInfo) error {
	if len(mountInfo.options) == 0 {
		return errors.New(`mount-control "options" cannot be empty`)
	}
	if err := validateNoAppArmorRegexpWithError(`cannot use mount-control "option" attribute`, mountInfo.options...); err != nil {
		return err
	}
	var types []string
	if mountInfo.hasType() {
		if incompatibleOption := optionIncompatibleWithFsType(mountInfo.options); incompatibleOption != "" {
			return fmt.Errorf(`mount-control option %q is incompatible with specifying filesystem type`, incompatibleOption)
		}
		types = mountInfo.types
	} else {
		types = defaultFSTypes
	}
	for _, o := range mountInfo.options {
		if strutil.ListContains(allowedKernelMountOptions, o) {
			continue
		}
		optionName := strings.SplitAfter(o, "=")[0] // for options with arguments, validate only option
		if strutil.ListContains(allowedUserspaceMountOptions, optionName) {
			continue
		}
		if isAllowedFilesystemSpecificMountOption(types, optionName) {
			continue
		}
		return fmt.Errorf(`mount-control option unrecognized or forbidden: %q`, o)
	}
	return nil
}

// Find the first option which is incompatible with a FS type declaration
func optionIncompatibleWithFsType(options []string) string {
	for _, o := range options {
		if strutil.ListContains(optionsWithoutFsType, o) {
			return o
		}
	}
	return ""
}

func isAllowedFilesystemSpecificMountOption(types []string, optionName string) bool {
	for _, fstype := range types {
		option := optionName
		if strutil.ListContains(filesystemsWithColonSeparatedOptions, fstype) {
			option = strings.SplitAfter(optionName, ":")[0]
		}
		fsAllowedOptions := allowedFilesystemSpecificMountOptions[fstype]
		if !strutil.ListContains(fsAllowedOptions, option) {
			return false
		}
	}
	return true
}

func validateMountInfo(mountInfo *MountInfo) error {
	if err := validateWhatAttr(mountInfo); err != nil {
		return err
	}

	if err := validateWhereAttr(mountInfo.where); err != nil {
		return err
	}

	if err := validateMountTypes(mountInfo.types); err != nil {
		return err
	}

	if err := validateMountOptions(mountInfo); err != nil {
		return err
	}

	// Until we have a clear picture of how this should work, disallow creating
	// persistent mounts into $SNAP_DATA
	if mountInfo.persistent && strings.HasPrefix(mountInfo.where, "$SNAP_DATA") {
		return errors.New(`mount-control "persistent" attribute cannot be used to mount onto $SNAP_DATA`)
	}

	return nil
}

// Create a new list containing only the allowed kernel options from the given
// options
func filterAllowedKernelMountOptions(options []string) []string {
	var filtered []string
	for _, opt := range options {
		if strutil.ListContains(allowedKernelMountOptions, opt) {
			filtered = append(filtered, opt)
		}
	}
	return filtered
}

func (iface *mountControlInterface) BeforeConnectPlug(plug *interfaces.ConnectedPlug) error {
	// The systemd.ListMountUnits() method works by issuing the command
	// "systemctl show *.mount", but globbing was only added in systemd v209.
	if err := systemd.EnsureAtLeast(209); err != nil {
		return err
	}

	hasMountEntries := false
	err := enumerateMounts(plug, func(mountInfo *MountInfo) error {
		hasMountEntries = true
		return validateMountInfo(mountInfo)
	})
	if err != nil {
		return err
	}

	if !hasMountEntries {
		return mountAttrTypeError
	}

	return nil
}

func (iface *mountControlInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	mountControlSnippet := bytes.NewBuffer(nil)
	emit := func(f string, args ...interface{}) {
		fmt.Fprintf(mountControlSnippet, f, args...)
	}
	snapInfo := plug.Snap()

	emit(`
  # Rules added by the mount-control interface
  capability sys_admin,  # for mount

  owner @{PROC}/@{pid}/mounts r,
  owner @{PROC}/@{pid}/mountinfo r,
  owner @{PROC}/self/mountinfo r,

  /{,usr/}bin/mount ixr,
  /{,usr/}bin/umount ixr,
  # mount/umount (via libmount) track some mount info in these files
  /run/mount/utab* wrlk,
`)

	// No validation is occurring here, as it was already performed in
	// BeforeConnectPlug()
	enumerateMounts(plug, func(mountInfo *MountInfo) error {

		source := mountInfo.what
		target := mountInfo.where
		if target[0] == '$' {
			matches := whereRegexp.FindStringSubmatchIndex(target)
			if matches == nil || len(matches) < 4 {
				// This cannot really happen, as the string wouldn't pass the validation
				return fmt.Errorf(`internal error: "where" fails to match regexp: %q`, mountInfo.where)
			}
			// the first two elements in "matches" are the boundaries of the whole
			// string; the next two are the boundaries of the first match, which is
			// what we care about as it contains the environment variable we want
			// to expand:
			variableStart, variableEnd := matches[2], matches[3]
			variable := target[variableStart:variableEnd]
			expanded := snapInfo.ExpandSnapVariables(variable)
			target = expanded + target[variableEnd:]
		}

		var typeRule string
		if optionIncompatibleWithFsType(mountInfo.options) != "" {
			// In this rule the FS type will not match unless it's empty
			typeRule = ""
		} else {
			var types []string
			if mountInfo.hasType() {
				types = mountInfo.types
			} else {
				types = defaultFSTypes
			}
			typeRule = "fstype=(" + strings.Join(types, ",") + ")"
		}

		// only pass the allowed kernel mount options on to apparmor
		options := strings.Join(filterAllowedKernelMountOptions(mountInfo.options), ",")

		emit("  mount %s options=(%s) \"%s\" -> \"%s{,/}\",\n", typeRule, options, source, target)
		emit("  umount \"%s{,/}\",\n", target)
		return nil
	})

	spec.AddSnippet(mountControlSnippet.String())
	return nil
}

func (iface *mountControlInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	return true
}

func init() {
	registerIface(&mountControlInterface{
		commonInterface: commonInterface{
			name:                 "mount-control",
			summary:              mountControlSummary,
			baseDeclarationPlugs: mountControlBaseDeclarationPlugs,
			baseDeclarationSlots: mountControlBaseDeclarationSlots,
			implicitOnCore:       true,
			implicitOnClassic:    true,
			connectedPlugSecComp: mountControlConnectedPlugSecComp,
		},
	})
}
