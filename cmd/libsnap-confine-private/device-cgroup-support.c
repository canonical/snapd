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
#include <sys/stat.h>

#include "cgroup-support.h"
#include "cleanup-funcs.h"
#include "snap.h"
#include "string-utils.h"
#include "utils.h"

#ifdef ENABLE_BPF
#include "bpf/bpf-insn.h"
#include "bpf-support.h"
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
            int cgroup_fd;
            int devmap_fd;
            char *tag;
        } v2;
    };
};

__attribute__((format(printf, 2, 3))) static void sc_dprintf(int fd, const char *format, ...);

static int sc_udev_open_cgroup_v1(const char *security_tag, int flags, sc_cgroup_fds *fds);
static void sc_cleanup_cgroup_fds(sc_cgroup_fds *fds);

static int _sc_cgroup_v1_init(sc_device_cgroup *self, int flags) {
    self->v1.fds = sc_cgroup_fds_new();

    /* initialize to something sane */
    if (sc_udev_open_cgroup_v1(self->security_tag, flags, &self->v1.fds) < 0) {
        if ((flags & SC_DEVICE_CGROUP_FROM_EXISTING) != 0) {
            return -1;
        }
        die("cannot prepare cgroup v1 device hierarchy");
    }
    /* Deny device access by default.
     *
     * Write 'a' to devices.deny to remove all existing devices that were added
     * in previous launcher invocations, then add the static and assigned
     * devices. This ensures that at application launch the cgroup only has
     * what is currently assigned. */
    sc_dprintf(self->v1.fds.devices_deny_fd, "a");
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
    sc_dprintf(self->v1.fds.cgroup_procs_fd, "%i\n", getpid());
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

static void _sc_cgroup_v2_attach_pid(sc_device_cgroup *self, pid_t pid) {
    /* nothing to do here, the device controller is attached to the cgroup
     * already, and we are part of it */
}

#ifdef ENABLE_BPF
static int load_devcgroup_prog(int map_fd) {
    // Basic rules about registers:
    // r0    - return value of built in functions and exit code of the program
    // r1-r5 - respective arguments to built in functions, clobbered by calls
    // r6-r9 - general purpose, preserved by callees
    // r10   - read only, stack pointer
    // Stack is 512 bytes.
    //
    // The function declaration implementing the program looks like this:
    // int program(struct bpf_cgroup_dev_ctx * ctx)
    // where *ctx is passed in r1, while the result goes to r0
    //
    /* just a placeholder for map value */
    uint8_t map_value __attribute__((unused));
    // where the value is 1 byte, but effectively ignored at this time. We are
    // using the map as a set, but 0 sized key cannot be used when creating a
    // map.
    size_t key_start = 17;
    /* NOTE: we pull a nasty hack, the structure is packed and its size isn't
     * aligned to multiples of 4; if we place it on a stack at an address
     * aligned to 4 bytes, the starting offsets of major and minor would be
     * unaligned; however, the first field of the structure is 1 byte, so we can
     * put the structure at 4 byte aligned address -1 and thus major and minor
     * end up aligned without too much hassle */
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

static int _sc_cgroup_v2_init_bpf(sc_device_cgroup *self, int flags) {
    self->v2.devmap_fd = -1;
    self->v2.cgroup_fd = -1;

    char *own_group SC_CLEANUP(sc_cleanup_string) = sc_cgroup_v2_own_path_full();
    if (own_group == NULL) {
        die("cannot obtain own group path");
    }

    const bool from_existing = (flags & SC_DEVICE_CGROUP_FROM_EXISTING) != 0;

    char own_group_full[PATH_MAX] = {0};
    sc_must_snprintf(own_group_full, sizeof(own_group_full), "/sys/fs/cgroup/%s", own_group);
    int cgroup_fd = open(own_group_full, O_PATH | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW);
    if (cgroup_fd < 0) {
        die("cannot open own cgroup directory %s", own_group_full);
    }
    debug("cgroup %s opened at %d", own_group_full, cgroup_fd);

    self->v2.tag = sc_strdup(self->security_tag);
    /* bpffs is unhappy about dots in the name, replace all with underscores */
    for (char *c = strchr(self->v2.tag, '.'); c != NULL; c = strchr(c, '.')) {
        *c = '_';
    }

    char path[PATH_MAX] = {0};
    sc_must_snprintf(path, sizeof path, "/sys/fs/bpf/snap/%s", self->v2.tag);

    /* TODO: open("/sys/fs/bpf") and then mkdirat?  */
    sc_identity old = sc_set_effective_identity(sc_root_group_identity());
    if (mkdir("/sys/fs/bpf/snap", 0700) < 0) {
        if (errno != EEXIST) {
            die("cannot create /sys/fs/bpf/snap directory");
        }
    }
    (void)sc_set_effective_identity(old);

    /* XXX: this should be more than enough keys */
    const size_t max_entries = 500;
    int devmap_fd = bpf_get_by_path(path);
    if (devmap_fd < 0) {
        if (errno != ENOENT) {
            die("cannot get existing device map");
        }
        if (from_existing) {
            /* there is no map, and we haven't been asked to setup a new cgroup */
            return -1;
        }
        debug("device map not present yet");
        /* map not created and pinned yet */
        const size_t value_size = 1;
        devmap_fd = bpf_create_map(BPF_MAP_TYPE_HASH, sizeof(struct sc_cgroup_v2_device_key), value_size, max_entries);
        if (devmap_fd < 0) {
            die("cannot create bpf map");
        }
        debug("got bpf map at fd: %d", devmap_fd);
        sc_identity old = sc_set_effective_identity(sc_root_group_identity());
        if (bpf_pin_to_path(devmap_fd, path) < 0) {
            /* we checked that the map did not exist, so fail on EEXIST too */
            die("cannot pin map to %s", path);
        }
        (void)sc_set_effective_identity(old);
    } else if (!from_existing) {
        /* the devices access map exists, and we have been asked to setup a cgroup */

        debug("found existing device map");
        /* the v1 implementation blocks all devices by default and then adds
         * each assigned one individually, however for v2 there's no way to drop
         * all the contents of the map*/

        /* first collect all keys in the map */
        sc_cgroup_v2_device_key *existing_keys = calloc(max_entries, sizeof(sc_cgroup_v2_device_key));
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
        int prog_fd = load_devcgroup_prog(devmap_fd);

        if (bpf_prog_attach(BPF_CGROUP_DEVICE, cgroup_fd, prog_fd) < 0) {
            die("cannot attach cgroup program");
        }
    }

    self->v2.devmap_fd = devmap_fd;
    self->v2.cgroup_fd = cgroup_fd;

    return 0;
}

static void _sc_cgroup_v2_close_bpf(sc_device_cgroup *self) {
    sc_cleanup_string(&self->v2.tag);
    /* the map is pinned to a per-snap-application file and referenced by the
     * program */
    sc_cleanup_close(&self->v2.devmap_fd);
    sc_cleanup_close(&self->v2.cgroup_fd);
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
#endif	/* ENABLE_BPF */

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
         * creating the directory if necessary. Note that we always chown the
         * resulting directory to root:root. */
        sc_identity old = sc_set_effective_identity(sc_root_group_identity());
        if (mkdirat(devices_fd, security_tag_relpath, 0755) < 0) {
            if (errno != EEXIST) {
                die("cannot create directory %s/%s/%s", cgroup_path, devices_relpath, security_tag_relpath);
            }
        }
        (void)sc_set_effective_identity(old);
    }

    int SC_CLEANUP(sc_cleanup_close) security_tag_fd = -1;
    security_tag_fd = openat(devices_fd, security_tag_relpath, O_RDONLY | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW);
    if (security_tag_fd < 0) {
        if (from_existing && errno == ENOENT) {
            return -1;
        }
        die("cannot open %s/%s/%s", cgroup_path, devices_relpath, security_tag_relpath);
    }

    /* Open devices.allow relative to /sys/fs/cgroup/devices/snap.$SNAP_NAME.$APP_NAME */
    const char *devices_allow_relpath = "devices.allow";
    int SC_CLEANUP(sc_cleanup_close) devices_allow_fd = -1;
    devices_allow_fd = openat(security_tag_fd, devices_allow_relpath, O_WRONLY | O_CLOEXEC | O_NOFOLLOW);
    if (devices_allow_fd < 0) {
        if (from_existing && errno == ENOENT) {
            return -1;
        }
        die("cannot open %s/%s/%s/%s", cgroup_path, devices_relpath, security_tag_relpath, devices_allow_relpath);
    }

    /* Open devices.deny relative to /sys/fs/cgroup/devices/snap.$SNAP_NAME.$APP_NAME */
    const char *devices_deny_relpath = "devices.deny";
    int SC_CLEANUP(sc_cleanup_close) devices_deny_fd = -1;
    devices_deny_fd = openat(security_tag_fd, devices_deny_relpath, O_WRONLY | O_CLOEXEC | O_NOFOLLOW);
    if (devices_deny_fd < 0) {
        if (from_existing && errno == ENOENT) {
            return -1;
        }
        die("cannot open %s/%s/%s/%s", cgroup_path, devices_relpath, security_tag_relpath, devices_deny_relpath);
    }

    /* Open cgroup.procs relative to /sys/fs/cgroup/devices/snap.$SNAP_NAME.$APP_NAME */
    const char *cgroup_procs_relpath = "cgroup.procs";
    int SC_CLEANUP(sc_cleanup_close) cgroup_procs_fd = -1;
    cgroup_procs_fd = openat(security_tag_fd, cgroup_procs_relpath, O_WRONLY | O_CLOEXEC | O_NOFOLLOW);
    if (cgroup_procs_fd < 0) {
        if (from_existing && errno == ENOENT) {
            return -1;
        }
        die("cannot open %s/%s/%s/%s", cgroup_path, devices_relpath, security_tag_relpath, cgroup_procs_relpath);
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
