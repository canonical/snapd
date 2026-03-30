/*
 * Copyright (C) 2016 Canonical Ltd
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

#include "ns-support.h"
#include "ns-support.c"

#include "../libsnap-confine-private/cleanup-funcs.h"
#include "../libsnap-confine-private/test-utils.h"

#include <errno.h>
#include <linux/magic.h>  // for NSFS_MAGIC
#include <sys/utsname.h>
#include <sys/vfs.h>

#include <glib.h>
#include <glib/gstdio.h>

// Set alternate namespace directory
static void sc_set_ns_dir(const char *dir) { sc_ns_dir = dir; }

// Set alternate managed CA certs directory.
static void sc_set_managed_ca_certs_dir(const char *dir) { sc_managed_ca_certs_dir = dir; }

// Set alternate system CA certs directory.
static void sc_set_system_ca_certs_dir(const char *dir) { sc_system_ca_certs_dir = dir; }

// A variant of unsetenv that is compatible with GDestroyNotify
static void my_unsetenv(const char *k) { unsetenv(k); }

// Use temporary directory for namespace groups.
//
// The directory is automatically reset to the real value at the end of the
// test.
static const char *sc_test_use_fake_ns_dir(void) {
    char *ns_dir = NULL;
    if (g_test_subprocess()) {
        // Check if the environment variable is set. If so then someone is already
        // managing the temporary directory and we should not create a new one.
        ns_dir = getenv("SNAP_CONFINE_NS_DIR");
        g_assert_nonnull(ns_dir);
    } else {
        ns_dir = g_dir_make_tmp(NULL, NULL);
        g_assert_nonnull(ns_dir);
        g_test_queue_free(ns_dir);
        g_assert_cmpint(setenv("SNAP_CONFINE_NS_DIR", ns_dir, 0), ==, 0);
        g_test_queue_destroy((GDestroyNotify)my_unsetenv, "SNAP_CONFINE_NS_DIR");
        g_test_queue_destroy((GDestroyNotify)rm_rf_tmp, ns_dir);
    }
    g_test_queue_destroy((GDestroyNotify)sc_set_ns_dir, SC_NS_DIR);
    sc_set_ns_dir(ns_dir);
    return ns_dir;
}

// Use a temporary directory for the managed CA certs path.
static const char *sc_test_use_fake_managed_ca_certs_dir(void) {
    char *managed_dir = g_dir_make_tmp(NULL, NULL);
    char *managed_link = g_build_filename(managed_dir, "merged", NULL);
    g_assert_nonnull(managed_dir);
    g_test_queue_free(managed_dir);
    g_test_queue_free(managed_link);
    g_test_queue_destroy((GDestroyNotify)rm_rf_tmp, managed_dir);
    g_test_queue_destroy((GDestroyNotify)sc_set_managed_ca_certs_dir, (gpointer)SC_MANAGED_CA_CERTS_DIR);
    sc_set_managed_ca_certs_dir(managed_link);
    return managed_dir;
}

// Check that allocating a namespace group sets up internal data structures to
// safe values.
static void test_sc_alloc_mount_ns(void) {
    struct sc_mount_ns *group = NULL;
    group = sc_alloc_mount_ns();
    g_test_queue_free(group);
    g_assert_nonnull(group);
    g_assert_cmpint(group->dir_fd, ==, -1);
    g_assert_cmpint(group->pipe_master[0], ==, -1);
    g_assert_cmpint(group->pipe_master[1], ==, -1);
    g_assert_cmpint(group->pipe_helper[0], ==, -1);
    g_assert_cmpint(group->pipe_helper[1], ==, -1);
    g_assert_cmpint(group->child, ==, 0);
    g_assert_null(group->name);
}

// Initialize a namespace group.
//
// The group is automatically destroyed at the end of the test.
static struct sc_mount_ns *sc_test_open_mount_ns(const char *group_name) {
    // Initialize a namespace group
    struct sc_mount_ns *group = NULL;
    if (group_name == NULL) {
        group_name = "test-group";
    }
    group = sc_open_mount_ns(group_name);
    g_test_queue_destroy((GDestroyNotify)sc_close_mount_ns, group);
    // Check if the returned group data looks okay
    g_assert_nonnull(group);
    g_assert_cmpint(group->dir_fd, !=, -1);
    g_assert_cmpint(group->pipe_master[0], ==, -1);
    g_assert_cmpint(group->pipe_master[1], ==, -1);
    g_assert_cmpint(group->pipe_helper[0], ==, -1);
    g_assert_cmpint(group->pipe_helper[1], ==, -1);
    g_assert_cmpint(group->child, ==, 0);
    g_assert_cmpstr(group->name, ==, group_name);
    return group;
}

// Check that initializing a namespace group creates the appropriate
// filesystem structure.
static void test_sc_open_mount_ns(void) {
    const char *ns_dir = sc_test_use_fake_ns_dir();
    sc_test_open_mount_ns(NULL);
    // Check that the group directory exists
    g_assert_true(g_file_test(ns_dir, G_FILE_TEST_EXISTS | G_FILE_TEST_IS_DIR));
}

// Sanity check, ensure that the namespace filesystem identifier is what we
// expect, aka NSFS_MAGIC.
static void test_nsfs_fs_id(void) {
    struct utsname uts;
    if (uname(&uts) < 0) {
        g_test_message("cannot use uname(2)");
        g_test_fail();
        return;
    }
    int major, minor;
    if (sscanf(uts.release, "%d.%d", &major, &minor) != 2) {
        g_test_message("cannot use sscanf(2) to parse kernel release");
        g_test_fail();
        return;
    }
    if (major < 3 || (major == 3 && minor < 19)) {
        g_test_skip("this test needs kernel 3.19+");
        return;
    }
    struct statfs buf;
    int err = statfs("/proc/self/ns/mnt", &buf);
    g_assert_cmpint(err, ==, 0);
    g_assert_cmpint(buf.f_type, ==, NSFS_MAGIC);
}

// Check that sc_running_kernel_is_6_18() agrees with a straightforward
// uname(2) reading done independently here, so that the two never drift
// apart from each other on whatever kernel the test happens to run on.
static void test_sc_running_kernel_is_6_18(void) {
    struct utsname uts;
    g_assert_cmpint(uname(&uts), ==, 0);
    int major = 0, minor = 0;
    g_assert_cmpint(sscanf(uts.release, "%d.%d", &major, &minor), ==, 2);
    bool expected = major == 6 && minor == 18;
    g_assert_cmpint(sc_running_kernel_is_6_18(), ==, expected);
}

// Check that sc_ensure_mount_ns_id_ordered() enforces its precondition that
// a helper process has already been forked for this group.
static void test_sc_ensure_mount_ns_id_ordered_no_helper(void) {
    struct sc_mount_ns *group = sc_alloc_mount_ns();
    g_test_queue_free(group);

    if (g_test_subprocess()) {
        sc_ensure_mount_ns_id_ordered(group);
        g_assert_not_reached();
    }
    g_test_trap_subprocess(NULL, 0, 0);
    g_test_trap_assert_failed();
    g_test_trap_assert_stderr("*precondition failed: we don't have a helper process*");
}

// Check that sc_read_mnt_ns_id() dies on a genuine ioctl failure (as opposed
// to ENOTTY/EINVAL, which mean "not supported" and are reported via a false
// return instead, see the comment above the function).
static void test_sc_read_mnt_ns_id_bad_fd(void) {
    if (g_test_subprocess()) {
        uint64_t ns_id = 0;
        // -1 is never a valid file descriptor, so the ioctl below fails with
        // EBADF, which sc_read_mnt_ns_id() must treat as a real error.
        sc_read_mnt_ns_id(-1, &ns_id);
        g_assert_not_reached();
    }
    g_test_trap_subprocess(NULL, 0, 0);
    g_test_trap_assert_failed();
    g_test_trap_assert_stderr("*cannot query mount namespace id*");
}

// Check that sc_read_mnt_ns_id() can read our own, real mount namespace id.
// This exercises the ioctl wrapper without needing any special privilege:
// every process can read /proc/self/ns/mnt.
static void test_sc_read_mnt_ns_id_self(void) {
    int fd = open("/proc/self/ns/mnt", O_RDONLY | O_CLOEXEC);
    g_assert_cmpint(fd, !=, -1);
    uint64_t ns_id = 0;
    bool supported = sc_read_mnt_ns_id(fd, &ns_id);
    close(fd);
    if (!supported) {
        // NS_GET_ID is unavailable on kernels older than the ones affected
        // by the ordering bug this whole file works around.
        g_test_skip("NS_GET_ID is not supported by this kernel");
        return;
    }
    g_assert_cmpuint(ns_id, !=, 0);
}

// Check the common case end-to-end: a helper forked just before we unshare
// a fresh mount namespace should, overwhelmingly, already be ordered before
// it, and sc_ensure_mount_ns_id_ordered() should return without needing to
// exercise its CPU-sweep fallback (which, if it did trigger, would still be
// expected to terminate -- see the comment above the function). On kernels
// outside the affected 6.18.y series this is a no-op by construction (see
// sc_running_kernel_is_6_18()), so the test still passes there, it just
// doesn't exercise anything beyond that early return.
//
// This needs real CAP_SYS_ADMIN in the same user namespace as the helper
// process, like the real code always has by the time it gets here: using
// unshare(CLONE_NEWUSER) to fake privilege, as some other tests in this
// suite do to run unprivileged, does not work here specifically, because it
// would leave the helper behind in a different (the real, original) user
// namespace, and opening its /proc/<pid>/ns/mnt across that boundary fails
// with EACCES regardless of what capabilities we hold in our new one.
static void test_sc_ensure_mount_ns_id_ordered_common_case(void) {
    if (geteuid() != 0) {
        g_test_skip("this test only runs as root");
        return;
    }

    struct sc_mount_ns *group = sc_alloc_mount_ns();
    g_test_queue_free(group);

    pid_t pid = fork();
    g_assert_cmpint(pid, !=, -1);
    if (pid == 0) {
        // helper: stay in the namespace we were forked in and wait to be
        // killed by the parent.
        prctl(PR_SET_PDEATHSIG, SIGKILL, 0, 0, 0);
        pause();
        _exit(0);
    }
    group->child = pid;

    g_assert_cmpint(unshare(CLONE_NEWNS), ==, 0);

    sc_ensure_mount_ns_id_ordered(group);

    kill(pid, SIGKILL);
    int status = 0;
    waitpid(pid, &status, 0);
}

// Helper: write content to a file under the given directory.
static void write_file(const char *dir, const char *name, const char *content) {
    char path[PATH_MAX] = {0};
    sc_must_snprintf(path, sizeof path, "%s/%s", dir, name);
    FILE *f = fopen(path, "w");
    g_assert_nonnull(f);
    if (content != NULL) {
        fputs(content, f);
    }
    fclose(f);
}

static char *create_fake_managed_generation(const char *managed_dir, const char *generation) {
    char *published_dir = g_build_filename(managed_dir, "published", generation, NULL);
    g_assert_cmpint(g_mkdir_with_parents(published_dir, 0755), ==, 0);

    char *merged = g_build_filename(managed_dir, "merged", NULL);
    char *target = g_build_filename("published", generation, NULL);
    g_assert_cmpint(symlink(target, merged), ==, 0);
    g_free(target);
    g_free(merged);
    return published_dir;
}

// When the info file does not exist and there is no current generation, the
// namespace can be reused.
static void test_managed_ca_cert_db_changed__no_info_file(void) {
    const char *ns_dir = sc_test_use_fake_ns_dir();
    const char *managed_dir = sc_test_use_fake_managed_ca_certs_dir();
    (void)ns_dir;
    (void)managed_dir;

    sc_invocation inv = {.snap_instance = "test-snap"};
    g_assert_false(managed_ca_cert_db_changed(&inv));
}

// When the info file exists but no generation is recorded while the host now
// exposes one, the namespace must be recreated.
static void test_managed_ca_cert_db_changed__no_generation_key(void) {
    const char *ns_dir = sc_test_use_fake_ns_dir();
    const char *managed_dir = sc_test_use_fake_managed_ca_certs_dir();

    char *published_dir = create_fake_managed_generation(managed_dir, "gen-1");
    g_test_queue_free(published_dir);

    write_file(ns_dir, "snap.test-snap.info", "base-snap-name=core24\n");

    sc_invocation inv = {.snap_instance = "test-snap"};
    g_assert_true(managed_ca_cert_db_changed(&inv));
}

// When the recorded generation matches the current generation, the function
// should return false.
static void test_managed_ca_cert_db_changed__generation_unchanged(void) {
    const char *ns_dir = sc_test_use_fake_ns_dir();
    const char *managed_dir = sc_test_use_fake_managed_ca_certs_dir();

    char *published_dir = create_fake_managed_generation(managed_dir, "gen-1");
    g_test_queue_free(published_dir);

    g_test_queue_destroy((GDestroyNotify)sc_set_system_ca_certs_dir, (gpointer)SC_SYSTEM_CA_CERTS_DIR);
    sc_set_system_ca_certs_dir(published_dir);

    char info_content[512] = {0};
    snprintf(info_content, sizeof info_content, "base-snap-name=core24\nmanaged-ca-certs-generation=gen-1\n");
    write_file(ns_dir, "snap.test-snap.info", info_content);

    sc_invocation inv = {.snap_instance = "test-snap"};
    g_assert_false(managed_ca_cert_db_changed(&inv));
}

// When the generation is unchanged but the namespace still exposes some other
// /etc/ssl/certs directory, the preserved namespace must be recreated.
static void test_managed_ca_cert_db_changed__generation_unchanged_missing_mount(void) {
    const char *ns_dir = sc_test_use_fake_ns_dir();
    const char *managed_dir = sc_test_use_fake_managed_ca_certs_dir();

    char *published_dir = create_fake_managed_generation(managed_dir, "gen-1");
    g_test_queue_free(published_dir);

    char *base_certs = g_build_filename(managed_dir, "base-certs", NULL);
    g_assert_cmpint(g_mkdir_with_parents(base_certs, 0755), ==, 0);
    g_test_queue_free(base_certs);

    g_test_queue_destroy((GDestroyNotify)sc_set_system_ca_certs_dir, (gpointer)SC_SYSTEM_CA_CERTS_DIR);
    sc_set_system_ca_certs_dir(base_certs);

    write_file(ns_dir, "snap.test-snap.info", "base-snap-name=core24\nmanaged-ca-certs-generation=gen-1\n");

    sc_invocation inv = {.snap_instance = "test-snap"};
    g_assert_true(managed_ca_cert_db_changed(&inv));
}

// When the host generation differs from what was recorded, the function
// should return true.
static void test_managed_ca_cert_db_changed__generation_changed(void) {
    const char *ns_dir = sc_test_use_fake_ns_dir();
    const char *managed_dir = sc_test_use_fake_managed_ca_certs_dir();

    char *published_dir = create_fake_managed_generation(managed_dir, "gen-2");
    g_test_queue_free(published_dir);

    write_file(ns_dir, "snap.test-snap.info", "base-snap-name=core24\nmanaged-ca-certs-generation=gen-1\n");

    sc_invocation inv = {.snap_instance = "test-snap"};
    g_assert_true(managed_ca_cert_db_changed(&inv));
}

// When a generation was recorded but the current managed CA cert path is not a
// symlink anymore, the namespace must be recreated.
static void test_managed_ca_cert_db_changed__legacy_directory_layout(void) {
    const char *ns_dir = sc_test_use_fake_ns_dir();
    const char *managed_dir = sc_test_use_fake_managed_ca_certs_dir();

    char *merged = g_build_filename(managed_dir, "merged", NULL);
    g_assert_cmpint(g_mkdir_with_parents(merged, 0755), ==, 0);
    g_test_queue_free(merged);

    write_file(ns_dir, "snap.test-snap.info", "base-snap-name=core24\nmanaged-ca-certs-generation=gen-1\n");

    sc_invocation inv = {.snap_instance = "test-snap"};
    g_assert_true(managed_ca_cert_db_changed(&inv));
}

static void __attribute__((constructor)) init(void) {
    g_test_add_func("/ns/sc_alloc_mount_ns", test_sc_alloc_mount_ns);
    g_test_add_func("/ns/sc_open_mount_ns", test_sc_open_mount_ns);
    g_test_add_func("/ns/nsfs_fs_id", test_nsfs_fs_id);
    g_test_add_func("/ns/sc_running_kernel_is_6_18", test_sc_running_kernel_is_6_18);
    g_test_add_func("/ns/sc_ensure_mount_ns_id_ordered_no_helper", test_sc_ensure_mount_ns_id_ordered_no_helper);
    g_test_add_func("/ns/sc_read_mnt_ns_id_bad_fd", test_sc_read_mnt_ns_id_bad_fd);
    g_test_add_func("/ns/sc_read_mnt_ns_id_self", test_sc_read_mnt_ns_id_self);
    g_test_add_func("/ns/sc_ensure_mount_ns_id_ordered_common_case", test_sc_ensure_mount_ns_id_ordered_common_case);
    g_test_add_func("/ns/managed_ca_cert_db_changed/no_info_file", test_managed_ca_cert_db_changed__no_info_file);
    g_test_add_func("/ns/managed_ca_cert_db_changed/no_generation_key",
                    test_managed_ca_cert_db_changed__no_generation_key);
    g_test_add_func("/ns/managed_ca_cert_db_changed/generation_unchanged",
                    test_managed_ca_cert_db_changed__generation_unchanged);
    g_test_add_func("/ns/managed_ca_cert_db_changed/generation_unchanged_missing_mount",
                    test_managed_ca_cert_db_changed__generation_unchanged_missing_mount);
    g_test_add_func("/ns/managed_ca_cert_db_changed/generation_changed",
                    test_managed_ca_cert_db_changed__generation_changed);
    g_test_add_func("/ns/managed_ca_cert_db_changed/legacy_directory_layout",
                    test_managed_ca_cert_db_changed__legacy_directory_layout);
}
