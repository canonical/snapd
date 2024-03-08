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
#include "config.h"

#include <errno.h>
#include <fcntl.h>
#include <stdarg.h>
#include <string.h>
#include <sys/resource.h>
#include <sys/stat.h>

#include "cgroup-support.h"
#include "cleanup-funcs.h"
#include "snap.h"
#include "string-utils.h"
#include "utils.h"

#ifdef ENABLE_BPF
#include "bpf-support.h"
#include "bpf/bpf-insn.h"
#endif
#include "device-cgroup-support.h"

typedef struct sc_cgroup_fds {
    int devices_allow_fd;
    int devices_deny_fd;
    int cgroup_procs_fd;
} sc_cgroup_fds;

static sc_cgroup_fds sc_cgroup_fds_new(void) {
    /* Note that -1 is the neutral value for a file descriptor.
     * This is relevant as a cleanup handler for sc_cgroup_fds,
     * closes all file descriptors that are not -1. */
    sc_cgroup_fds empty = {-1, -1, -1};
    return empty;
}

struct sc_device_cgroup {
    bool is_v2;
    char *security_tag;
    union {
        struct {
            sc_cgroup_fds fds;
        } v1;
        struct {
            int devmap_fd;
            int prog_fd;
            char *tag;
            struct rlimit old_limit;
        } v2;
    };
};

__attribute__((format(printf, 2, 3))) static void sc_dprintf(int fd, const char *format, ...);

static int sc_udev_open_cgroup_v1(const char *security_tag, int flags, sc_cgroup_fds *fds);
static void sc_cleanup_cgroup_fds(sc_cgroup_fds *fds);

static int _sc_cgroup_v1_init(sc_device_cgroup *self, int flags) {
    self->v1.fds = sc_cgroup_fds_new();

    /* are we creating the group or just using whatever there is? */
    const bool from_existing = (flags & SC_DEVICE_CGROUP_FROM_EXISTING) != 0;
    /* initialize to something sane */
    if (sc_udev_open_cgroup_v1(self->security_tag, flags, &self->v1.fds) < 0) {
        if (from_existing) {
            return -1;
        }
        die("cannot prepare cgroup v1 device hierarchy");
    }
    /* Only deny devices if we are not using an existing group -
     * if we deny devices for an existing group that we just opened,
     * we risk denying access to a device that a currently running process
     * is about to access and should legitimately have access to.
     * A concrete example of this is when this function is used by snap-device-helper
     * when a new udev device event is triggered and we are adding that device
     * to the snap's device cgroup. At this point, a running application may be
     * accessing other devices which it should have access to (such as /dev/null
     * or one of the other common, default devices) we would deny access to that
     * existing device by re-creating the allow list of devices every time.
     * */
    if (!from_existing) {
        /* starting a device cgroup from scratch, so deny device access by
         * default.
         *
         * Write 'a' to devices.deny to remove all existing devices that were added
         * in previous launcher invocations, then add the static and assigned
         * devices. This ensures that at application launch the cgroup only has
         * what is currently assigned. */
        sc_dprintf(self->v1.fds.devices_deny_fd, "a");
    }
    return 0;
}

static void _sc_cgroup_v1_close(sc_device_cgroup *self) { sc_cleanup_cgroup_fds(&self->v1.fds); }

static void _sc_cgroup_v1_action(int fd, int kind, int major, int minor) {
    if ((uint32_t)minor != SC_DEVICE_MINOR_ANY) {
        sc_dprintf(fd, "%c %u:%u rwm\n", (kind == S_IFCHR) ? 'c' : 'b', major, minor);
    } else {
        /* use a mask to allow/deny all minor devices for that major */
        sc_dprintf(fd, "%c %u:* rwm\n", (kind == S_IFCHR) ? 'c' : 'b', major);
    }
}

static void _sc_cgroup_v1_allow(sc_device_cgroup *self, int kind, int major, int minor) {
    _sc_cgroup_v1_action(self->v1.fds.devices_allow_fd, kind, major, minor);
}

static void _sc_cgroup_v1_deny(sc_device_cgroup *self, int kind, int major, int minor) {
    _sc_cgroup_v1_action(self->v1.fds.devices_deny_fd, kind, major, minor);
}

static void _sc_cgroup_v1_attach_pid(sc_device_cgroup *self, pid_t pid) {
    sc_dprintf(self->v1.fds.cgroup_procs_fd, "%i\n", pid);
}

/**
 * sc_cgroup_v2_device_key is the key in the map holding allowed devices
 */
struct sc_cgroup_v2_device_key {
    uint8_t type;
    uint32_t major;
    uint32_t minor;
} __attribute__((packed));
typedef struct sc_cgroup_v2_device_key sc_cgroup_v2_device_key;

/**
 * sc_cgroup_v2_device_value holds the value stored in the map
 *
 * Note that this type is just a helper, the map cannot be used as a set with 0
 * sized value so we always store something in it (specifically value 1) in the
 * map.
 */
typedef uint8_t sc_cgroup_v2_device_value;

#ifdef ENABLE_BPF
static int load_devcgroup_prog(int map_fd) {
    /* Basic rules about registers:
     * r0    - return value of built in functions and exit code of the program
     * r1-r5 - respective arguments to built in functions, clobbered by calls
     * r6-r9 - general purpose, preserved by callees
     * r10   - read only, stack pointer
     * Stack is 512 bytes.
     *
     * The function declaration implementing a device cgroup program looks like
     * this:
     *   int program(struct bpf_cgroup_dev_ctx * ctx)
     * where *ctx is passed in r1, while the result goes to r0
     */

    /* just a placeholder for map value where the value is 1 byte, but
     * effectively ignored at this time. Ideally it should be possible to use
     * the map as a set with 0 sized key, but this is currently unsupported by
     * the kernel (as of 5.13) */
    sc_cgroup_v2_device_value map_value __attribute__((unused));
    /* we need to place the key structure on the stack and pull a nasty hack
     * here, the structure is packed and its size isn't aligned to multiples of
     * 4; if we place it on a stack at an address aligned to 4 bytes, the
     * starting offsets of major and minor would be unaligned; however, the
     * first field of the structure is 1 byte, so we can put the structure at 4
     * byte aligned address -1 and thus major and minor end up aligned without
     * too much hassle; since we are doing the stack management ourselves have
     * the key structure start at the offset that meets the alignment properties
     * described above and such that the whole structure fits on the stack (even
     * with some spare room) */
    size_t key_start = 17;
    struct bpf_insn prog[] = {
        /* r1 holds pointer to bpf_cgroup_dev_ctx */
        /* initialize r0 */
        BPF_MOV64_IMM(BPF_REG_0, 0), /* r0 = 0 */
        /* make some place on the stack for the key */
        BPF_MOV64_REG(BPF_REG_6, BPF_REG_10), /* r6 = r10 (sp) */
        /* r6 = where the key starts on the stack */
        BPF_ALU64_IMM(BPF_ADD, BPF_REG_6, -key_start), /* r6 = sp + (-key start offset) */
        /* copy major to our key */
        BPF_LDX_MEM(BPF_W, BPF_REG_2, BPF_REG_1,
                    offsetof(struct bpf_cgroup_dev_ctx, major)), /* r2 = *(u32)(r1->major) */
        BPF_STX_MEM(BPF_W, BPF_REG_6, BPF_REG_2,
                    offsetof(struct sc_cgroup_v2_device_key, major)), /* *(r6 + offsetof(major)) = r2 */
        /* copy minor to our key */
        BPF_LDX_MEM(BPF_W, BPF_REG_2, BPF_REG_1,
                    offsetof(struct bpf_cgroup_dev_ctx, minor)), /* r2 = *(u32)(r1->minor) */
        BPF_STX_MEM(BPF_W, BPF_REG_6, BPF_REG_2,
                    offsetof(struct sc_cgroup_v2_device_key, minor)), /* *(r6 + offsetof(minor)) = r2 */
        /* copy device access_type to r2 */
        BPF_LDX_MEM(BPF_W, BPF_REG_2, BPF_REG_1,
                    offsetof(struct bpf_cgroup_dev_ctx, access_type)), /* r2 = *(u32*)(r1->access_type) */
        /* access_type is encoded as (BPF_DEVCG_ACC_* << 16) | BPF_DEVCG_DEV_*,
         * but we only care about type */
        BPF_ALU32_IMM(BPF_AND, BPF_REG_2, 0xffff), /* r2 = r2 & 0xffff */
        /* is it a block device? */
        BPF_JMP_IMM(BPF_JNE, BPF_REG_2, BPF_DEVCG_DEV_BLOCK, 2), /* if (r2 != BPF_DEVCG_DEV_BLOCK) goto pc + 2 */
        BPF_ST_MEM(BPF_B, BPF_REG_6, offsetof(struct sc_cgroup_v2_device_key, type),
                   'b'), /* *(uint8*)(r6->type) = 'b' */
        BPF_JMP_A(5),
        BPF_JMP_IMM(BPF_JNE, BPF_REG_2, BPF_DEVCG_DEV_CHAR, 2), /* if (r2 != BPF_DEVCG_DEV_CHAR) goto pc + 2 */
        BPF_ST_MEM(BPF_B, BPF_REG_6, offsetof(struct sc_cgroup_v2_device_key, type),
                   'c'), /* *(uint8*)(r6->type) = 'c' */
        BPF_JMP_A(2),
        /* unknown device type */
        BPF_MOV64_IMM(BPF_REG_0, 0), /* r0 = 0 */
        BPF_EXIT_INSN(),
        /* back on happy path, prepare arguments for map lookup */
        BPF_LD_MAP_FD(BPF_REG_1, map_fd),
        BPF_MOV64_REG(BPF_REG_2, BPF_REG_6),                                 /* r2 = (struct key *) r6, */
        BPF_RAW_INSN(BPF_JMP | BPF_CALL, 0, 0, 0, BPF_FUNC_map_lookup_elem), /* r0 = bpf_map_lookup_elem(<map>,
                                                                                &key) */
        BPF_JMP_IMM(BPF_JEQ, BPF_REG_0, 0, 1),                               /* if (value_ptr == 0) goto pc + 1 */
        /* we found an exact match */
        BPF_JMP_A(5), /* else goto pc + 5 */
        /* maybe the minor number is using 0xffffffff (any) mask */
        BPF_ST_MEM(BPF_W, BPF_REG_6, offsetof(struct sc_cgroup_v2_device_key, minor), UINT32_MAX),
        BPF_LD_MAP_FD(BPF_REG_1, map_fd),
        BPF_MOV64_REG(BPF_REG_2, BPF_REG_6),                                 /* r2 = (struct key *) r6, */
        BPF_RAW_INSN(BPF_JMP | BPF_CALL, 0, 0, 0, BPF_FUNC_map_lookup_elem), /* r0 = bpf_map_lookup_elem(<map>,
                                                                                &key) */
        BPF_JMP_IMM(BPF_JEQ, BPF_REG_0, 0, 2),                               /* if (value_ptr == 0) goto pc + 2 */
        /* we found a match with any minor number for that type|major */
        BPF_MOV64_IMM(BPF_REG_0, 1), /* r0 = 1 */
        BPF_JMP_A(1),
        BPF_MOV64_IMM(BPF_REG_0, 0), /* r0 = 0 */
        BPF_EXIT_INSN(),
    };

    char log_buf[4096] = {0};

    int prog_fd =
        bpf_load_prog(BPF_PROG_TYPE_CGROUP_DEVICE, prog, sizeof(prog) / sizeof(prog[0]), log_buf, sizeof(log_buf));
    if (prog_fd < 0) {
        die("cannot load program:\n%s\n", log_buf);
    }
    return prog_fd;
}

static void _sc_cleanup_v2_device_key(sc_cgroup_v2_device_key **keyptr) {
    if (keyptr == NULL || *keyptr == NULL) {
        return;
    }
    free(*keyptr);
    *keyptr = NULL;
}

static void _sc_cgroup_v2_set_memlock_limit(struct rlimit limit) {
    /* we may be setting the limit over the current max, which requires root
     * privileges or CAP_SYS_RESOURCE */
    if (setrlimit(RLIMIT_MEMLOCK, &limit) < 0) {
        die("cannot set memlock limit to %llu:%llu", (long long unsigned int)limit.rlim_cur,
            (long long unsigned int)limit.rlim_max);
    }
}

// _sc_cgroup_v2_adjust_memlock_limit updates the memlock limit which used to be
// consulted by pre 5.11 kernels when creating BPF maps or loading BPF programs.
// It has been observed that some systems (eg. Debian using 5.10 kernel) have
// the default limit set to 64k, which combined with an older way of accounting
// of memory use by BPF objects, renders snap-confine unable to create the BPF
// map. The situation is made worse by the fact that there is no right value
// here, for example older systemd set the limit to 64MB while newer versions
// set it even higher). Returns the old limit setting.
static struct rlimit _sc_cgroup_v2_adjust_memlock_limit(void) {
    struct rlimit old_limit = {0};

    if (getrlimit(RLIMIT_MEMLOCK, &old_limit) < 0) {
        die("cannot obtain the current memlock limit");
    }
    /* this should be more than enough for creating the map and loading the
     * filtering program */
    const rlim_t min_memlock_limit = 512 * 1024;
    if (old_limit.rlim_max >= min_memlock_limit) {
        return old_limit;
    }
    debug("adjusting memlock limit to %llu", (long long unsigned int)min_memlock_limit);
    struct rlimit limit = {
        .rlim_cur = min_memlock_limit,
        .rlim_max = min_memlock_limit,
    };
    _sc_cgroup_v2_set_memlock_limit(limit);
    return old_limit;
}

static bool _sc_is_snap_cgroup(const char *group) {
    /* make a copy as basename may modify its input */
    char copy[PATH_MAX] = {0};
    strncpy(copy, group, sizeof(copy) - 1);
    char *leaf = basename(copy);
    if (!sc_startswith(leaf, "snap.")) {
        return false;
    }
    if (!sc_endswith(leaf, ".service") && !sc_endswith(leaf, ".scope")) {
        return false;
    }
    return true;
}

static int _sc_cgroup_v2_init_bpf(sc_device_cgroup *self, int flags) {
    self->v2.devmap_fd = -1;
    self->v2.prog_fd = -1;

    /* fix the memlock limit if needed, this affects creating maps */
    self->v2.old_limit = _sc_cgroup_v2_adjust_memlock_limit();

    const bool from_existing = (flags & SC_DEVICE_CGROUP_FROM_EXISTING) != 0;

    self->v2.tag = sc_strdup(self->security_tag);
    /* bpffs is unhappy about dots in the name, replace all with underscores */
    for (char *c = strchr(self->v2.tag, '.'); c != NULL; c = strchr(c, '.')) {
        *c = '_';
    }

    char path[PATH_MAX] = {0};
    static const char bpf_base[] = "/sys/fs/bpf";
    sc_must_snprintf(path, sizeof path, "%s/snap/%s", bpf_base, self->v2.tag);

    /* we expect bpffs to be mounted at /sys/fs/bpf, which should have been done
     * by systemd, but some systems out there are a weird mix of older userland
     * and new kernels, in which case the assumptions about the state of the
     * system no longer hold and we may need to mount bpffs ourselves */
    if (!bpf_path_is_bpffs("/sys/fs/bpf")) {
        debug("/sys/fs/bpf is not a bpffs mount");
        /* bpffs isn't mounted at the usual place, or die if that fails */
        bpf_mount_bpffs("/sys/fs/bpf");
        debug("bpffs mounted at /sys/fs/bpf");
    }

    /* Using 0000 permissions to avoid a race condition; we'll set the right
     * permissions after chmod. */
    int bpf_fd = open(bpf_base, O_PATH | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
    if (bpf_fd < 0) {
        die("cannot open %s", bpf_base);
    }

    if (mkdirat(bpf_fd, "snap", 0000) == 0) {
        /* the new directory must be owned by root:root. */
        if (fchownat(bpf_fd, "snap", 0, 0, AT_SYMLINK_NOFOLLOW) < 0) {
            die("cannot set root ownership on %s/snap directory", bpf_base);
        }
        if (fchmodat(bpf_fd, "snap", 0700, AT_SYMLINK_NOFOLLOW) < 0) {
            /* On Debian, this fails with "operation not supported. But it
             * should not be a critical error, we can also leave with 0000
             * permissions. */
            if (errno != ENOTSUP) {
                die("cannot set 0700 permissions on %s/snap directory", bpf_base);
            }
        }
    } else if (errno != EEXIST) {
        die("cannot create %s/snap directory", bpf_base);
    }
    close(bpf_fd);

    /* and obtain a file descriptor to the map, also as root */
    int devmap_fd = bpf_get_by_path(path);
    /* keep a copy of errno in case it gets clobbered */
    int get_by_path_errno = errno;
    /* XXX: this should be more than enough keys */
    const size_t max_entries = 500;
    if (devmap_fd < 0) {
        if (get_by_path_errno != ENOENT) {
            die("cannot get existing device map");
        }
        if (from_existing) {
            debug("device map not present, not creating one");
            /* restore the errno so that the caller sees ENOENT */
            errno = get_by_path_errno;
            /* there is no map, and we haven't been asked to setup a new cgroup */
            return -1;
        }
        debug("device map not present yet");
        /* map not created and pinned yet */
        const size_t value_size = 1;
        /* kernels used to do account of BPF memory using rlimit memlock pool,
         * thus on older kernels (seen on 5.10), the map effectively locks 11
         * pages (45k) of memlock memory, while on newer kernels (5.11+) only 2 (8k) */
        /* NOTE: the new file map must be owned by root:root. */
        devmap_fd = bpf_create_map(BPF_MAP_TYPE_HASH, sizeof(struct sc_cgroup_v2_device_key), value_size, max_entries);
        if (devmap_fd < 0) {
            die("cannot create bpf map");
        }
        debug("got bpf map at fd: %d", devmap_fd);
        /* the map can only be referenced by a fd like object which is valid
         * here and referenced by the BPF program that we'll load; by pinning
         * the map to a well known path, it is possible to obtain a reference to
         * it from another process, which is used by snap-device-helper to
         * dynamically update device access permissions; the downside is a tiny
         * bit of kernel memory still in use as, even once all BPF programs
         * referencing the map go away with their respective cgroups, the map
         * will stay around as it is still referenced by the path */
        if (bpf_pin_to_path(devmap_fd, path) < 0) {
            /* we checked that the map did not exist, so fail on EEXIST too */
            die("cannot pin map to %s", path);
        }
    } else if (!from_existing) {
        /* the devices access map exists, and we have been asked to setup a
         * cgroup, so clear the old map first so it was like it never existed */

        debug("found existing device map");
        /* the v1 implementation blocks all devices by default and then adds
         * each assigned one individually, however for v2 there's no way to drop
         * all the contents of the map, so we need to find out what keys are
         * there in the map */

        /* first collect all keys in the map */
        sc_cgroup_v2_device_key *existing_keys SC_CLEANUP(_sc_cleanup_v2_device_key) =
            calloc(max_entries, sizeof(sc_cgroup_v2_device_key));
        if (existing_keys == NULL) {
            die("cannot allocate keys map");
        }
        /* 'current' key is zeroed, such that no entry can match it and thus
         * we'll iterate over the keys from the beginning */
        sc_cgroup_v2_device_key key = {0};
        size_t existing_count = 0;
        while (true) {
            sc_cgroup_v2_device_key next = {0};
            if (existing_count >= max_entries) {
                die("too many elements in the map");
            }
            if (existing_count > 0) {
                /* grab the previous key */
                key = existing_keys[existing_count - 1];
            }
            int ret = bpf_map_get_next_key(devmap_fd, &key, &next);
            if (ret == -1) {
                if (errno != ENOENT) {
                    die("cannot lookup existing device map keys");
                }
                /* we are done */
                break;
            }
            existing_keys[existing_count] = next;
            existing_count++;
        }
        debug("found %zu existing entries in devices map", existing_count);
        if (existing_count > 0) {
#if 0
            /* XXX: we should be doing a batch delete of elements, however:
             * - on Arch with 5.13 kernel I'm getting EINVAL
             * - the linux/bpf.h header present during build on 16.04 does not
             *     support batch operations
             */
            if (bpf_map_delete_batch(devmap_fd, existing_keys, existing_count) < 0) {
                 die("cannot dump all elements from devices map");
            }
#endif
            for (size_t i = 0; i < existing_count; i++) {
                sc_cgroup_v2_device_key key = existing_keys[i];
                debug("delete key for %c %d:%d", key.type, key.major, key.minor);
                if (bpf_map_delete_elem(devmap_fd, &key) < 0) {
                    die("cannot delete device map entry for %c %d:%d", key.type, key.major, key.minor);
                }
            }
        }
    }

    if (!from_existing) {
        /* load and attach the BPF program */
        int prog_fd = load_devcgroup_prog(devmap_fd);
        /* keep track of the program */
        self->v2.prog_fd = prog_fd;
    }

    self->v2.devmap_fd = devmap_fd;

    return 0;
}

static void _sc_cgroup_v2_close_bpf(sc_device_cgroup *self) {
    /* restore the old limit */
    _sc_cgroup_v2_set_memlock_limit(self->v2.old_limit);

    sc_cleanup_string(&self->v2.tag);
    /* the map is pinned to a per-snap-application file and referenced by the
     * program */
    sc_cleanup_close(&self->v2.devmap_fd);
    sc_cleanup_close(&self->v2.prog_fd);
}

static void _sc_cgroup_v2_allow_bpf(sc_device_cgroup *self, int kind, int major, int minor) {
    struct sc_cgroup_v2_device_key key = {
        .major = major,
        .minor = minor,
        .type = (kind == S_IFCHR) ? 'c' : 'b',
    };
    sc_cgroup_v2_device_value value = 1;
    debug("v2 allow %c %u:%u", (char)key.type, key.major, key.minor);
    if (bpf_update_map(self->v2.devmap_fd, &key, &value) < 0) {
        die("cannot update device map for key %c %u:%u", key.type, key.major, key.minor);
    }
}

static void _sc_cgroup_v2_deny_bpf(sc_device_cgroup *self, int kind, int major, int minor) {
    struct sc_cgroup_v2_device_key key = {
        .major = major,
        .minor = minor,
        .type = (kind == S_IFCHR) ? 'c' : 'b',
    };
    debug("v2 deny %c %u:%u", (char)key.type, key.major, key.minor);
    if (bpf_map_delete_elem(self->v2.devmap_fd, &key) < 0 && errno != ENOENT) {
        die("cannot delete device map entry for key %c %u:%u", key.type, key.major, key.minor);
    }
}

static void _sc_cgroup_v2_attach_pid_bpf(sc_device_cgroup *self, pid_t pid) {
    /* we are setting up device filtering for ourselves */
    if (pid != getpid()) {
        die("internal error: cannot attach device cgroup to other process than current");
    }
    if (self->v2.prog_fd == -1) {
        die("internal error: BPF program not loaded");
    }

    char *own_group SC_CLEANUP(sc_cleanup_string) = sc_cgroup_v2_own_path_full();
    if (own_group == NULL) {
        die("cannot obtain own group path");
    }
    debug("process in cgroup %s", own_group);

    if (!_sc_is_snap_cgroup(own_group)) {
        /* we cannot proceed to install a device filtering program when the
         * process is not in a snap specific cgroup, as we would effectively
         * lock down the group that can be shared with other processes or even
         * the whole desktop session */
        die("%s is not a snap cgroup", own_group);
    }

    char own_group_full_path[PATH_MAX] = {0};
    sc_must_snprintf(own_group_full_path, sizeof(own_group_full_path), "/sys/fs/cgroup/%s", own_group);

    int cgroup_fd SC_CLEANUP(sc_cleanup_close) = -1;
    cgroup_fd = open(own_group_full_path, O_PATH | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW);
    if (cgroup_fd < 0) {
        die("cannot open own cgroup directory %s", own_group_full_path);
    }
    debug("cgroup %s opened at %d", own_group_full_path, cgroup_fd);

    /* attach the program to the cgroup */
    int attach = bpf_prog_attach(BPF_CGROUP_DEVICE, cgroup_fd, self->v2.prog_fd);
    if (attach < 0) {
        die("cannot attach cgroup program");
    }
}
#endif /* ENABLE_BPF */

static void _sc_cgroup_v2_close(sc_device_cgroup *self) {
#ifdef ENABLE_BPF
    _sc_cgroup_v2_close_bpf(self);
#endif
}

static void _sc_cgroup_v2_allow(sc_device_cgroup *self, int kind, int major, int minor) {
#ifdef ENABLE_BPF
    _sc_cgroup_v2_allow_bpf(self, kind, major, minor);
#else
    die("device cgroup v2 is not enabled");
#endif
}

static void _sc_cgroup_v2_deny(sc_device_cgroup *self, int kind, int major, int minor) {
#ifdef ENABLE_BPF
    _sc_cgroup_v2_deny_bpf(self, kind, major, minor);
#else
    die("device cgroup v2 is not enabled");
#endif
}

static void _sc_cgroup_v2_attach_pid(sc_device_cgroup *self, pid_t pid) {
#ifdef ENABLE_BPF
    _sc_cgroup_v2_attach_pid_bpf(self, pid);
#else
    die("device cgroup v2 is not enabled");
#endif
}

static int _sc_cgroup_v2_init(sc_device_cgroup *self, int flags) {
#ifdef ENABLE_BPF
    return _sc_cgroup_v2_init_bpf(self, flags);
#else
    if ((flags & SC_DEVICE_CGROUP_FROM_EXISTING) != 0) {
        errno = ENOSYS;
        return -1;
    }
    die("device cgroup v2 is not enabled");
    return -1;
#endif
}

static void sc_device_cgroup_close(sc_device_cgroup *self);

sc_device_cgroup *sc_device_cgroup_new(const char *security_tag, int flags) {
    sc_device_cgroup *self = calloc(1, sizeof(sc_device_cgroup));
    if (self == NULL) {
        die("cannot allocate device cgroup wrapper");
    }
    self->is_v2 = sc_cgroup_is_v2();
    self->security_tag = sc_strdup(security_tag);

    int ret = 0;
    if (self->is_v2) {
        ret = _sc_cgroup_v2_init(self, flags);
    } else {
        ret = _sc_cgroup_v1_init(self, flags);
    }

    if (ret < 0) {
        sc_device_cgroup_close(self);
        return NULL;
    }
    return self;
}

static void sc_device_cgroup_close(sc_device_cgroup *self) {
    if (self->is_v2) {
        _sc_cgroup_v2_close(self);
    } else {
        _sc_cgroup_v1_close(self);
    }
    sc_cleanup_string(&self->security_tag);
    free(self);
}

void sc_device_cgroup_cleanup(sc_device_cgroup **self) {
    if (*self == NULL) {
        return;
    }
    sc_device_cgroup_close(*self);
    *self = NULL;
}

int sc_device_cgroup_allow(sc_device_cgroup *self, int kind, int major, int minor) {
    if (kind != S_IFCHR && kind != S_IFBLK) {
        die("unsupported device kind 0x%04x", kind);
    }
    if (self->is_v2) {
        _sc_cgroup_v2_allow(self, kind, major, minor);
    } else {
        _sc_cgroup_v1_allow(self, kind, major, minor);
    }
    return 0;
}

int sc_device_cgroup_deny(sc_device_cgroup *self, int kind, int major, int minor) {
    if (kind != S_IFCHR && kind != S_IFBLK) {
        die("unsupported device kind 0x%04x", kind);
    }
    if (self->is_v2) {
        _sc_cgroup_v2_deny(self, kind, major, minor);
    } else {
        _sc_cgroup_v1_deny(self, kind, major, minor);
    }
    return 0;
}

int sc_device_cgroup_attach_pid(sc_device_cgroup *self, pid_t pid) {
    if (self->is_v2) {
        _sc_cgroup_v2_attach_pid(self, pid);
    } else {
        _sc_cgroup_v1_attach_pid(self, pid);
    }
    return 0;
}

static void sc_dprintf(int fd, const char *format, ...) {
    va_list ap1;
    va_list ap2;
    int n_expected, n_actual;

    va_start(ap1, format);
    va_copy(ap2, ap1);
    n_expected = vsnprintf(NULL, 0, format, ap2);
    n_actual = vdprintf(fd, format, ap1);
    if (n_actual == -1 || n_expected != n_actual) {
        die("cannot write to fd %d", fd);
    }
    va_end(ap2);
    va_end(ap1);
}

static int sc_udev_open_cgroup_v1(const char *security_tag, int flags, sc_cgroup_fds *fds) {
    /* Open /sys/fs/cgroup */
    const char *cgroup_path = "/sys/fs/cgroup";
    int SC_CLEANUP(sc_cleanup_close) cgroup_fd = -1;
    cgroup_fd = open(cgroup_path, O_PATH | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW);
    if (cgroup_fd < 0) {
        die("cannot open %s", cgroup_path);
    }

    const bool from_existing = (flags & SC_DEVICE_CGROUP_FROM_EXISTING) != 0;
    /* Open devices relative to /sys/fs/cgroup */
    const char *devices_relpath = "devices";
    int SC_CLEANUP(sc_cleanup_close) devices_fd = -1;
    devices_fd = openat(cgroup_fd, devices_relpath, O_PATH | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW);
    if (devices_fd < 0) {
        die("cannot open %s/%s", cgroup_path, devices_relpath);
    }

    const char *security_tag_relpath = security_tag;
    if (!from_existing) {
        /* Open snap.$SNAP_NAME.$APP_NAME relative to /sys/fs/cgroup/devices,
         * creating the directory if necessary.
         * Using 0000 permissions to avoid a race condition; we'll set the
         * right permissions after chmod. */
        if (mkdirat(devices_fd, security_tag_relpath, 0000) == 0) {
            /* the new directory must be owned by root:root. */
            if (fchownat(devices_fd, security_tag_relpath, 0, 0, AT_SYMLINK_NOFOLLOW) < 0) {
                die("cannot set root ownership on %s/%s/%s", cgroup_path, devices_relpath, security_tag_relpath);
            }
            if (fchmodat(devices_fd, security_tag_relpath, 0755, 0) < 0) {
                die("cannot set 0755 permissions on %s/%s/%s", cgroup_path, devices_relpath, security_tag_relpath);
            }
        } else if (errno != EEXIST) {
            die("cannot create directory %s/%s/%s", cgroup_path, devices_relpath, security_tag_relpath);
        }
    }

    int SC_CLEANUP(sc_cleanup_close) security_tag_fd = -1;
    security_tag_fd = openat(devices_fd, security_tag_relpath, O_RDONLY | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW);
    if (security_tag_fd < 0) {
        if (from_existing && errno == ENOENT) {
            return -1;
        }
        die("cannot open %s/%s/%s", cgroup_path, devices_relpath, security_tag_relpath);
    }

    int SC_CLEANUP(sc_cleanup_close) devices_allow_fd = -1;
    int SC_CLEANUP(sc_cleanup_close) devices_deny_fd = -1;
    int SC_CLEANUP(sc_cleanup_close) cgroup_procs_fd = -1;

    /* Open device files relative to /sys/fs/cgroup/devices/snap.$SNAP_NAME.$APP_NAME */
    struct device_file_t {
        int *fd;
        const char *relpath;
    } device_files[] = {{&devices_allow_fd, "devices.allow"},
                        {&devices_deny_fd, "devices.deny"},
                        {&cgroup_procs_fd, "cgroup.procs"},
                        {NULL, NULL}};

    for (struct device_file_t *device_file = device_files; device_file->fd != NULL; device_file++) {
        int fd = openat(security_tag_fd, device_file->relpath, O_WRONLY | O_CLOEXEC | O_NOFOLLOW);
        if (fd < 0) {
            if (from_existing && errno == ENOENT) {
                return -1;
            }
            die("cannot open %s/%s/%s/%s", cgroup_path, devices_relpath, security_tag_relpath, device_file->relpath);
        }
        *device_file->fd = fd;
    }

    /* Everything worked so pack the result and "move" the descriptors over so
     * that they are not closed by the cleanup functions associated with the
     * individual variables. */
    fds->devices_allow_fd = devices_allow_fd;
    fds->devices_deny_fd = devices_deny_fd;
    fds->cgroup_procs_fd = cgroup_procs_fd;
    /* Reset the locals so that they are not closed by the cleanup handlers. */
    devices_allow_fd = -1;
    devices_deny_fd = -1;
    cgroup_procs_fd = -1;
    return 0;
}

static void sc_cleanup_cgroup_fds(sc_cgroup_fds *fds) {
    if (fds != NULL) {
        sc_cleanup_close(&fds->devices_allow_fd);
        sc_cleanup_close(&fds->devices_deny_fd);
        sc_cleanup_close(&fds->cgroup_procs_fd);
    }
}
