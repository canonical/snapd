/*
 * Copyright (C) 2017 Canonical Ltd
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

#include "classic-private.h"

#include <glib.h>
#include <unistd.h>

/* restore_os_release is an internal helper for mock_os_release */
static void restore_os_release(gpointer mocked) {
    unlink(mocked);
    sc_set_os_release(sc_get_default_os_release());
}

/* mock_os_release replaces the presence and contents of /etc/os-release
   as seen by classic.c. The mocked value may be NULL to have the code refer
   to an absent file. */
static void mock_os_release(const char *mocked) {
    const char *mocked_path = "os-release.missing";

    if (mocked != NULL) {
        mocked_path = "os-release.test";
        g_assert_true(g_file_set_contents(mocked_path, mocked, -1, NULL));
    }

    sc_set_os_release(mocked_path);
    g_test_queue_destroy((GDestroyNotify)restore_os_release, (gpointer)mocked_path);
}

/* restore_meta_snap_yaml is an internal helper for mock_meta_snap_yaml */
static void restore_meta_snap_yaml(gpointer mocked) {
    unlink(mocked);
    sc_set_meta_snap_yaml(sc_get_default_meta_snap_yaml());
}

/* mock_meta_snap_yaml replaces the presence and contents of /meta/snap.yaml
   as seen by classic.c. The mocked value may be NULL to have the code refer
   to an absent file. */
static void mock_meta_snap_yaml(const char *mocked) {
    const char *mocked_path = "snap-yaml.missing";

    if (mocked != NULL) {
        mocked_path = "snap-yaml.test";
        g_assert_true(g_file_set_contents(mocked_path, mocked, -1, NULL));
    }

    sc_set_meta_snap_yaml(mocked_path);
    g_test_queue_destroy((GDestroyNotify)restore_meta_snap_yaml, (gpointer)mocked_path);
}

static const char *os_release_classic =
    ""
    "NAME=\"Ubuntu\"\n"
    "VERSION=\"17.04 (Zesty Zapus)\"\n"
    "ID=ubuntu\n"
    "ID_LIKE=debian\n";

static void test_is_on_classic(void) {
    mock_os_release(os_release_classic);
    mock_meta_snap_yaml(NULL);
    g_assert_cmpint(sc_classify_distro(), ==, SC_DISTRO_CLASSIC);
}

static const char *os_release_core16 =
    ""
    "NAME=\"Ubuntu Core\"\n"
    "VERSION_ID=\"16\"\n"
    "ID=ubuntu-core\n";

static const char *meta_snap_yaml_core16 =
    ""
    "name: core\n"
    "version: 16-something\n"
    "type: core\n"
    "architectures: [amd64]\n";

static void test_is_on_core_on16(void) {
    mock_os_release(os_release_core16);
    mock_meta_snap_yaml(meta_snap_yaml_core16);
    g_assert_cmpint(sc_classify_distro(), ==, SC_DISTRO_CORE16);
}

static const char *os_release_core18 =
    ""
    "NAME=\"Ubuntu Core\"\n"
    "VERSION_ID=\"18\"\n"
    "ID=ubuntu-core\n";

static const char *meta_snap_yaml_core18 =
    ""
    "name: core18\n"
    "type: base\n"
    "architectures: [amd64]\n";

static void test_is_on_core_on18(void) {
    mock_os_release(os_release_core18);
    mock_meta_snap_yaml(meta_snap_yaml_core18);
    g_assert_cmpint(sc_classify_distro(), ==, SC_DISTRO_CORE_OTHER);
}

const char *os_release_core20 =
    ""
    "NAME=\"Ubuntu Core\"\n"
    "VERSION_ID=\"20\"\n"
    "ID=ubuntu-core\n";

static const char *meta_snap_yaml_core20 =
    ""
    "name: core20\n"
    "type: base\n"
    "architectures: [amd64]\n";

static void test_is_on_core_on20(void) {
    mock_os_release(os_release_core20);
    mock_meta_snap_yaml(meta_snap_yaml_core20);
    g_assert_cmpint(sc_classify_distro(), ==, SC_DISTRO_CORE_OTHER);
}

static const char *os_release_classic_with_long_line =
    ""
    "NAME=\"Ubuntu\"\n"
    "VERSION=\"17.04 (Zesty Zapus)\"\n"
    "ID=ubuntu\n"
    "ID_LIKE=debian\n"
    "LONG=line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line."
    "line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line."
    "line.line.line.line.line.line.line.line.line.";

static void test_is_on_classic_with_long_line(void) {
    mock_os_release(os_release_classic_with_long_line);
    mock_meta_snap_yaml(NULL);
    g_assert_cmpint(sc_classify_distro(), ==, SC_DISTRO_CLASSIC);
}

static const char *os_release_fedora_base =
    ""
    "NAME=Fedora\nID=fedora\nVARIANT_ID=snappy\n";

static const char *meta_snap_yaml_fedora_base =
    ""
    "name: fedora29\n"
    "type: base\n"
    "architectures: [amd64]\n";

static void test_is_on_fedora_base(void) {
    mock_os_release(os_release_fedora_base);
    mock_meta_snap_yaml(meta_snap_yaml_fedora_base);
    g_assert_cmpint(sc_classify_distro(), ==, SC_DISTRO_CORE_OTHER);
}

static const char *os_release_fedora_ws =
    ""
    "NAME=Fedora\nID=fedora\nVARIANT_ID=workstation\n";

static void test_is_on_fedora_ws(void) {
    mock_os_release(os_release_fedora_ws);
    mock_meta_snap_yaml(NULL);
    g_assert_cmpint(sc_classify_distro(), ==, SC_DISTRO_CLASSIC);
}

static const char *os_release_custom =
    ""
    "NAME=\"Custom Distribution\"\nID=custom\n";

static const char *meta_snap_yaml_custom =
    ""
    "name: custom\n"
    "version: rolling\n"
    "summary: Runtime environment based on Custom Distribution\n"
    "type: base\n"
    "architectures: [amd64]\n";

static void test_is_on_custom_base(void) {
    mock_os_release(os_release_custom);

    /* Without /meta/snap.yaml we treat "Custom Distribution" as classic. */
    mock_meta_snap_yaml(NULL);
    g_assert_cmpint(sc_classify_distro(), ==, SC_DISTRO_CLASSIC);

    /* With /meta/snap.yaml we treat it as core instead. */
    mock_meta_snap_yaml(meta_snap_yaml_custom);
    g_assert_cmpint(sc_classify_distro(), ==, SC_DISTRO_CORE_OTHER);
}

static const char *os_release_debian_like_valid =
    ""
    "ID=my-fun-distro\n"
    "ID_LIKE=debian\n";

static const char *os_release_debian_like_quoted_valid =
    ""
    "ID=my-fun-distro\n"
    "ID_LIKE=\"debian\"\n";

/* actual debian only sets ID=debian */
static const char *os_release_actual_debian_valid = "ID=debian\n";

static const char *os_release_invalid = "garbage\n";

static void test_is_debian_like(void) {
    mock_os_release(os_release_debian_like_valid);
    g_assert_true(sc_is_debian_like());

    mock_os_release(os_release_debian_like_quoted_valid);
    g_assert_true(sc_is_debian_like());

    mock_os_release(os_release_actual_debian_valid);
    g_assert_true(sc_is_debian_like());

    mock_os_release(os_release_fedora_ws);
    g_assert_false(sc_is_debian_like());

    mock_os_release(os_release_invalid);
    g_assert_false(sc_is_debian_like());
}

static void __attribute__((constructor)) init(void) {
    g_test_add_func("/classic/on-classic", test_is_on_classic);
    g_test_add_func("/classic/on-classic-with-long-line", test_is_on_classic_with_long_line);
    g_test_add_func("/classic/on-core-on16", test_is_on_core_on16);
    g_test_add_func("/classic/on-core-on18", test_is_on_core_on18);
    g_test_add_func("/classic/on-core-on20", test_is_on_core_on20);
    g_test_add_func("/classic/on-fedora-base", test_is_on_fedora_base);
    g_test_add_func("/classic/on-fedora-ws", test_is_on_fedora_ws);
    g_test_add_func("/classic/on-custom-base", test_is_on_custom_base);
    g_test_add_func("/classic/is-debian-like", test_is_debian_like);
}
