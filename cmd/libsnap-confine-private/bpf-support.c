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

#include "bpf-support.h"

#include <errno.h>
#include <stddef.h>
#include <stdint.h>
#include <string.h>
#include <sys/mount.h>
#include <sys/syscall.h>
#include <sys/vfs.h>
#include <unistd.h>

#include "utils.h"

static int sys_bpf(enum bpf_cmd cmd, union bpf_attr *attr, size_t size) {
#ifdef SYS_bpf
    return syscall(SYS_bpf, cmd, attr, size);
#else
    errno = ENOSYS;
    return -1;
#endif
}

#define __ptr_as_u64(__x) ((uint64_t)(uintptr_t)__x)

int bpf_create_map(enum bpf_map_type type, size_t key_size, size_t value_size, size_t max_entries) {
    debug(
        "create bpf map of type 0x%x, key size %zu, value size %zu, entries "
        "%zu",
        type, key_size, value_size, max_entries);
    union bpf_attr attr;
    memset(&attr, 0, sizeof(attr));
    attr.map_type = type;
    attr.key_size = key_size;
    attr.value_size = value_size;
    attr.max_entries = max_entries;
    return sys_bpf(BPF_MAP_CREATE, &attr, sizeof(attr));
}

int bpf_update_map(int map_fd, const void *key, const void *value) {
    union bpf_attr attr;
    memset(&attr, 0, sizeof(attr));
    attr.map_fd = map_fd;
    attr.key = __ptr_as_u64(key);
    attr.value = __ptr_as_u64(value);
    /* update or create an existing element */
    attr.flags = BPF_ANY;
    return sys_bpf(BPF_MAP_UPDATE_ELEM, &attr, sizeof(attr));
}

int bpf_pin_to_path(int fd, const char *path) {
    debug("pin bpf object %d to path %s", fd, path);
    union bpf_attr attr;
    memset(&attr, 0, sizeof(attr));
    attr.bpf_fd = fd;
    /* pointer must be converted to a u64 */
    attr.pathname = __ptr_as_u64(path);

    return sys_bpf(BPF_OBJ_PIN, &attr, sizeof(attr));
}

int bpf_get_by_path(const char *path) {
    debug("get bpf object at path %s", path);
    union bpf_attr attr;
    memset(&attr, 0, sizeof(attr));
    /* pointer must be converted to a u64 */
    attr.pathname = __ptr_as_u64(path);

    return sys_bpf(BPF_OBJ_GET, &attr, sizeof(attr));
}

int bpf_load_prog(enum bpf_prog_type type, const struct bpf_insn *insns, size_t insns_cnt, char *log_buf,
                  size_t log_buf_size) {
    if (type == BPF_PROG_TYPE_UNSPEC) {
        errno = EINVAL;
        return -1;
    }
    debug("load program of type 0x%x, %zu instructions", type, insns_cnt);
    union bpf_attr attr;
    memset(&attr, 0, sizeof(attr));
    attr.prog_type = type;
    attr.insns = __ptr_as_u64(insns);
    attr.insn_cnt = (uint64_t)insns_cnt;
    attr.license = __ptr_as_u64("GPL");
    if (log_buf != NULL) {
        attr.log_buf = __ptr_as_u64(log_buf);
        attr.log_size = log_buf_size;
        attr.log_level = 1;
    }

    /* XXX: libbpf does a while loop checking for EAGAIN */
    /* XXX: do we need to handle E2BIG? */
    return sys_bpf(BPF_PROG_LOAD, &attr, sizeof(attr));
}

int bpf_prog_attach(enum bpf_attach_type type, int cgroup_fd, int prog_fd) {
    debug("attach type 0x%x program %d to cgroup %d", type, prog_fd, cgroup_fd);
    union bpf_attr attr;
    memset(&attr, 0, sizeof(attr));

    attr.attach_type = type;
    attr.target_fd = cgroup_fd;
    attr.attach_bpf_fd = prog_fd;

    return sys_bpf(BPF_PROG_ATTACH, &attr, sizeof(attr));
}

int bpf_map_get_next_key(int map_fd, const void *key, void *next_key) {
    debug("get next key for map %d", map_fd);
    union bpf_attr attr;
    memset(&attr, 0, sizeof(attr));

    attr.map_fd = map_fd;
    attr.key = __ptr_as_u64(key);
    attr.next_key = __ptr_as_u64(next_key);

    return sys_bpf(BPF_MAP_GET_NEXT_KEY, &attr, sizeof(attr));
}

int bpf_map_delete_batch(int map_fd, const void *keys, size_t cnt) {
#if 0
/*
 * XXX: batch operations don't seem to work with 5.13.10, getting -EINVAL
 * XXX: also batch operations are supported by recent kernels only
 */
    debug("batch delete in map %d keys cnt %zu", map_fd, cnt);
    union bpf_attr attr;
    memset(&attr, 0, sizeof(attr));

    attr.map_fd = map_fd;
    attr.batch.keys = __ptr_as_u64(keys);
    attr.batch.count = cnt;
    /* TODO: getting EINVAL? */
    int ret = sys_bpf(BPF_MAP_DELETE_BATCH, &attr, sizeof(attr));
    debug("returned count %d", attr.batch.count);
    return ret;
#endif
    errno = ENOSYS;
    return -1;
}

int bpf_map_delete_elem(int map_fd, const void *key) {
    debug("delete elem in map %d", map_fd);
    union bpf_attr attr;
    memset(&attr, 0, sizeof(attr));

    attr.map_fd = map_fd;
    attr.key = __ptr_as_u64(key);

    return sys_bpf(BPF_MAP_DELETE_ELEM, &attr, sizeof(attr));
}

#ifndef BPF_FS_MAGIC
#define BPF_FS_MAGIC 0xcafe4a11
#endif

bool bpf_path_is_bpffs(const char *path) {
    struct statfs fs;
    int res = statfs(path, &fs);
    if (res < 0) {
        if (errno == ENOENT) {
            /* no path at all */
            return false;
        }
        die("cannot check filesystem type of %s", path);
    }
    /* see statfs(2) notes on  __fsword_t */
    if ((unsigned int)fs.f_type == BPF_FS_MAGIC) {
        return true;
    }
    return false;
}

void bpf_mount_bpffs(const char *path) {
    /* systemd and bpftool disagree as to the propagation mode of bpffs mounts,
     * so go with the default which is a shared propagation and matches the
     * state of a freshly booted system */
    int res = mount("bpf", path, "bpf", 0, "mode=0700");
    if (res < 0) {
        die("cannot mount bpf filesystem under %s", path);
    }
}
