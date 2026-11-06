/*
 * Copyright (C) 2026 Canonical Ltd
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

#include "device-cgroup-support.c"

#include <glib.h>

#include "device-cgroup-support.h"

/* Mock implementations of the BPF functions from bpf-support.c. */

static struct mocks {
    size_t update_map_calls;
    /* Track keys deleted by bpf_map_delete_elem */
    sc_cgroup_v2_device_key deleted_keys[100];
    size_t deleted_count;
} mocks;

static void mocks_reset(void) {
    memset(&mocks, 0, sizeof(mocks));
}

int bpf_update_map(int map_fd, const void *key, const void *value) {
    (void)map_fd;
    (void)key;
    (void)value;
    mocks.update_map_calls++;
    return 0;
}

int bpf_pin_to_path(int fd, const char *path) {
    (void)fd;
    (void)path;
    return 0;
}

int bpf_get_by_path(const char *path) {
    (void)path;
    errno = ENOENT;
    return -1;
}

int bpf_create_map(enum bpf_map_type type, size_t key_size, size_t value_size, size_t max_entries,
                   const char *map_name) {
    (void)type;
    (void)key_size;
    (void)value_size;
    (void)max_entries;
    (void)map_name;
    return 42;
}

int bpf_load_prog(enum bpf_prog_type type, const struct bpf_insn *insns, size_t insns_cnt, char *log_buf,
                 size_t log_buf_size, const char *prog_name) {
    (void)type;
    (void)insns;
    (void)insns_cnt;
    (void)log_buf;
    (void)log_buf_size;
    (void)prog_name;
    return 43;
}

int bpf_prog_attach(enum bpf_attach_type type, int cgroup_fd, int prog_fd) {
    (void)type;
    (void)cgroup_fd;
    (void)prog_fd;
    return 0;
}

int bpf_map_delete_elem(int map_fd, const void *key) {
    (void)map_fd;
    if (mocks.deleted_count < SC_ARRAY_SIZE(mocks.deleted_keys)) {
        mocks.deleted_keys[mocks.deleted_count++] = *(const sc_cgroup_v2_device_key *)key;
    }
    return 0;
}

int bpf_map_get_next_key(int map_fd, const void *key, void *next_key) {
    (void)map_fd;
    (void)key;
    (void)next_key;
    errno = ENOENT;
    return -1;
}

int bpf_map_delete_batch(int map_fd, const void *keys, size_t cnt) {
    (void)map_fd;
    (void)keys;
    (void)cnt;
    return 0;
}

bool bpf_path_is_bpffs(const char *path) {
    (void)path;
    return true;
}

void bpf_mount_bpffs(const char *path) {
    (void)path;
}

/* End of BPF mocks. */

/* Helper to create a key. */
static sc_cgroup_v2_device_key make_key(uint8_t type, uint32_t major, uint32_t minor) {
    sc_cgroup_v2_device_key key = {
        .type = type,
        .major = major,
        .minor = minor,
    };
    return key;
}

/* Helper to create a device cgroup for testing. Allocates with calloc (like
 * sc_device_cgroup_new does) so that free_test_cgroup can free it. */
static sc_device_cgroup *make_test_cgroup(const char *tag) {
    sc_device_cgroup *self = calloc(1, sizeof(sc_device_cgroup));
    g_assert_nonnull(self);
    self->is_v2 = true;
    self->v2.devmap_fd = -1;
    self->v2.prog_fd = -1;
    self->v2.devcgroup_lock_fd = -1;
    self->security_tag = sc_strdup(tag);
    return self;
}

/* Helper to free a test cgroup without calling close() on mock fds. */
static void free_test_cgroup(sc_device_cgroup *self) {
    _sc_cleanup_v2_device_key(&self->v2.stale_keys);
    sc_cleanup_string(&self->security_tag);
    free(self);
}

/* Helper to populate stale_keys with a given set of keys. */
static void set_stale_keys(sc_device_cgroup *self, const sc_cgroup_v2_device_key *keys, size_t count) {
    const size_t max_entries = 1000;
    self->v2.stale_keys = calloc(max_entries, sizeof(sc_cgroup_v2_device_key));
    g_assert_nonnull(self->v2.stale_keys);
    for (size_t i = 0; i < count; i++) {
        self->v2.stale_keys[i] = keys[i];
    }
    self->v2.stale_count = count;
}

/* Helper to check if a stale key matches the given key fields. */
static bool stale_contains(sc_device_cgroup *self, uint8_t type, uint32_t major, uint32_t minor) {
    for (size_t i = 0; i < self->v2.stale_count; i++) {
        const sc_cgroup_v2_device_key *s = &self->v2.stale_keys[i];
        if (s->type == type && s->major == major && s->minor == minor) {
            return true;
        }
    }
    return false;
}

/* Test that allowing a device removes all duplicate copies from the stale set.
 *
 * This simulates the scenario where bpf_map_get_next_key restarted (because
 * an older snap-device-helper from the native package deleted the cursor key
 * concurrently) and the snapshot array contains the same key twice. The allow
 * call must remove every matching copy so the key is not pruned after attach.
 */
static void test_stale_keys_remove_all_duplicates(void) {
    sc_device_cgroup *self = make_test_cgroup("snap.test-snapd-sh-core24.sh");
    sc_cgroup_v2_device_key keys[] = {
        make_key('c', 1, 3),   /* /dev/null */
        make_key('c', 1, 5),   /* /dev/zero */
        make_key('c', 1, 3),   /* /dev/null (duplicate) */
        make_key('c', 1, 9),   /* /dev/urandom */
    };
    set_stale_keys(self, keys, 4);

    /* Allow /dev/null (c 1:3) — this should remove both copies from stale */
    mocks_reset();
    sc_device_cgroup_allow(self, S_IFCHR, 1, 3);

    /* bpf_update_map was called once */
    g_assert_cmpuint(mocks.update_map_calls, ==, 1);

    /* Both copies of c 1:3 must be gone from the stale set */
    g_assert_cmpuint(self->v2.stale_count, ==, 2);
    g_assert_false(stale_contains(self, 'c', 1, 3));

    /* The other keys must still be present */
    g_assert_true(stale_contains(self, 'c', 1, 5));
    g_assert_true(stale_contains(self, 'c', 1, 9));

    free_test_cgroup(self);
}

/* Test that allowing a device not in the stale set leaves it unchanged. */
static void test_stale_keys_allow_not_in_stale(void) {
    sc_device_cgroup *self = make_test_cgroup("snap.test-snapd-sh-core24.sh");
    sc_cgroup_v2_device_key keys[] = {
        make_key('c', 1, 3),
        make_key('c', 1, 5),
    };
    set_stale_keys(self, keys, 2);

    /* Allow a device that is not in the stale set */
    sc_device_cgroup_allow(self, S_IFCHR, 99, 99);

    /* Stale set should be unchanged */
    g_assert_cmpuint(self->v2.stale_count, ==, 2);
    g_assert_true(stale_contains(self, 'c', 1, 3));
    g_assert_true(stale_contains(self, 'c', 1, 5));
    g_assert_false(stale_contains(self, 'c', 99, 99));

    free_test_cgroup(self);
}

/* Test that allowing a device with a single copy in the stale set removes it. */
static void test_stale_keys_remove_single(void) {
    sc_device_cgroup *self = make_test_cgroup("snap.test-snapd-sh-core24.sh");
    sc_cgroup_v2_device_key keys[] = {
        make_key('c', 1, 3),
        make_key('c', 1, 5),
        make_key('c', 1, 9),
    };
    set_stale_keys(self, keys, 3);

    /* Allow /dev/zero (c 1:5) */
    sc_device_cgroup_allow(self, S_IFCHR, 1, 5);

    g_assert_cmpuint(self->v2.stale_count, ==, 2);
    g_assert_true(stale_contains(self, 'c', 1, 3));
    g_assert_false(stale_contains(self, 'c', 1, 5));
    g_assert_true(stale_contains(self, 'c', 1, 9));

    free_test_cgroup(self);
}

/* Test with triple duplicates. */
static void test_stale_keys_remove_triple_duplicates(void) {
    sc_device_cgroup *self = make_test_cgroup("snap.test-snapd-sh-core24.sh");
    sc_cgroup_v2_device_key keys[] = {
        make_key('c', 1, 3),
        make_key('c', 1, 3),
        make_key('c', 1, 5),
        make_key('c', 1, 3),
        make_key('c', 1, 9),
    };
    set_stale_keys(self, keys, 5);

    /* Allow /dev/null (c 1:3) — must remove all three copies */
    sc_device_cgroup_allow(self, S_IFCHR, 1, 3);

    g_assert_cmpuint(self->v2.stale_count, ==, 2);
    g_assert_false(stale_contains(self, 'c', 1, 3));
    g_assert_true(stale_contains(self, 'c', 1, 5));
    g_assert_true(stale_contains(self, 'c', 1, 9));

    free_test_cgroup(self);
}

/* Helper to check if a key was deleted via bpf_map_delete_elem. */
static bool was_deleted_from_map(uint8_t type, uint32_t major, uint32_t minor) {
    for (size_t i = 0; i < mocks.deleted_count; i++) {
        const sc_cgroup_v2_device_key *k = &mocks.deleted_keys[i];
        if (k->type == type && k->major == major && k->minor == minor) {
            return true;
        }
    }
    return false;
}

/* Test that allowing a subset of the stale keys removes only the remaining
 * stale entries from the BPF map during the prune step. */
static void test_prune_after_allow(void) {
    sc_device_cgroup *self = make_test_cgroup("snap.test-snapd-sh-core24.sh");
    sc_cgroup_v2_device_key keys[] = {
        make_key('c', 1, 3),   /* /dev/null */
        make_key('c', 1, 5),   /* /dev/zero */
        make_key('c', 1, 7),   /* /dev/full */
        make_key('c', 1, 9),   /* /dev/urandom */
    };
    set_stale_keys(self, keys, 4);

    /* Allow /dev/null and /dev/zero, removing them from stale */
    mocks_reset();
    sc_device_cgroup_allow(self, S_IFCHR, 1, 3);
    sc_device_cgroup_allow(self, S_IFCHR, 1, 5);

    /* /dev/full and /dev/urandom should still be stale */
    g_assert_cmpuint(self->v2.stale_count, ==, 2);
    g_assert_false(stale_contains(self, 'c', 1, 3));
    g_assert_false(stale_contains(self, 'c', 1, 5));
    g_assert_true(stale_contains(self, 'c', 1, 7));
    g_assert_true(stale_contains(self, 'c', 1, 9));

    /* No deletions yet — prune happens after attach */
    g_assert_cmpuint(mocks.deleted_count, ==, 0);

    /* Prune: only the un-allowed keys should be deleted */
    _sc_cgroup_v2_prune_stale_keys(self);

    g_assert_cmpuint(mocks.deleted_count, ==, 2);
    g_assert_true(was_deleted_from_map('c', 1, 7));
    g_assert_true(was_deleted_from_map('c', 1, 9));
    g_assert_false(was_deleted_from_map('c', 1, 3));
    g_assert_false(was_deleted_from_map('c', 1, 5));

    /* Stale set is now empty */
    g_assert_cmpuint(self->v2.stale_count, ==, 0);

    free_test_cgroup(self);
}

/* Test that allowing all stale keys results in no deletions after prune. */
static void test_prune_all_allowed(void) {
    sc_device_cgroup *self = make_test_cgroup("snap.test-snapd-sh-core24.sh");
    sc_cgroup_v2_device_key keys[] = {
        make_key('c', 1, 3),
        make_key('c', 1, 5),
    };
    set_stale_keys(self, keys, 2);

    /* Allow all stale keys */
    mocks_reset();
    sc_device_cgroup_allow(self, S_IFCHR, 1, 3);
    sc_device_cgroup_allow(self, S_IFCHR, 1, 5);

    g_assert_cmpuint(self->v2.stale_count, ==, 0);

    /* Prune: nothing to delete */
    _sc_cgroup_v2_prune_stale_keys(self);
    g_assert_cmpuint(mocks.deleted_count, ==, 0);

    free_test_cgroup(self);
}

/* Test that allowing none of the stale keys results in all being deleted. */
static void test_prune_none_allowed(void) {
    sc_device_cgroup *self = make_test_cgroup("snap.test-snapd-sh-core24.sh");
    sc_cgroup_v2_device_key keys[] = {
        make_key('c', 1, 3),
        make_key('c', 1, 5),
        make_key('c', 1, 9),
    };
    set_stale_keys(self, keys, 3);

    /* Allow a device not in the stale set */
    mocks_reset();
    sc_device_cgroup_allow(self, S_IFCHR, 99, 99);

    /* All stale keys should still be present */
    g_assert_cmpuint(self->v2.stale_count, ==, 3);

    /* Prune: all stale keys should be deleted */
    _sc_cgroup_v2_prune_stale_keys(self);
    g_assert_cmpuint(mocks.deleted_count, ==, 3);
    g_assert_true(was_deleted_from_map('c', 1, 3));
    g_assert_true(was_deleted_from_map('c', 1, 5));
    g_assert_true(was_deleted_from_map('c', 1, 9));

    free_test_cgroup(self);
}

/* Test that with duplicate stale keys, allowing the duplicated key removes
 * all copies, and only the remaining keys are pruned. */
static void test_prune_with_duplicates(void) {
    sc_device_cgroup *self = make_test_cgroup("snap.test-snapd-sh-core24.sh");
    sc_cgroup_v2_device_key keys[] = {
        make_key('c', 1, 3),   /* /dev/null */
        make_key('c', 1, 5),   /* /dev/zero */
        make_key('c', 1, 3),   /* /dev/null (duplicate) */
        make_key('c', 1, 9),   /* /dev/urandom */
        make_key('c', 1, 3),   /* /dev/null (duplicate) */
    };
    set_stale_keys(self, keys, 5);

    /* Allow /dev/null — all three copies should be removed from stale */
    mocks_reset();
    sc_device_cgroup_allow(self, S_IFCHR, 1, 3);

    g_assert_cmpuint(self->v2.stale_count, ==, 2);
    g_assert_false(stale_contains(self, 'c', 1, 3));
    g_assert_true(stale_contains(self, 'c', 1, 5));
    g_assert_true(stale_contains(self, 'c', 1, 9));

    /* Prune: only /dev/zero and /dev/urandom should be deleted */
    _sc_cgroup_v2_prune_stale_keys(self);
    g_assert_cmpuint(mocks.deleted_count, ==, 2);
    g_assert_true(was_deleted_from_map('c', 1, 5));
    g_assert_true(was_deleted_from_map('c', 1, 9));
    g_assert_false(was_deleted_from_map('c', 1, 3));

    free_test_cgroup(self);
}

static void __attribute__((constructor)) init(void) {
    g_test_add_func("/device-cgroup/stale_keys/remove_all_duplicates", test_stale_keys_remove_all_duplicates);
    g_test_add_func("/device-cgroup/stale_keys/allow_not_in_stale", test_stale_keys_allow_not_in_stale);
    g_test_add_func("/device-cgroup/stale_keys/remove_single", test_stale_keys_remove_single);
    g_test_add_func("/device-cgroup/stale_keys/remove_triple_duplicates", test_stale_keys_remove_triple_duplicates);
    g_test_add_func("/device-cgroup/prune/after_allow", test_prune_after_allow);
    g_test_add_func("/device-cgroup/prune/all_allowed", test_prune_all_allowed);
    g_test_add_func("/device-cgroup/prune/none_allowed", test_prune_none_allowed);
    g_test_add_func("/device-cgroup/prune/with_duplicates", test_prune_with_duplicates);
}
