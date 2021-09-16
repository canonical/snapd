AC_PREREQ([2.69])
AC_INIT([snap-confine], m4_esyscmd_s([cat VERSION]), [snapcraft@lists.ubuntu.com])
AC_CONFIG_SRCDIR([snap-confine/snap-confine.c])
AC_CONFIG_HEADERS([config.h])
AC_USE_SYSTEM_EXTENSIONS
AM_INIT_AUTOMAKE([foreign subdir-objects])
AM_MAINTAINER_MODE([enable])

# Checks for programs.
AC_PROG_CC_C99
AC_PROG_CPP
AC_PROG_INSTALL
AC_PROG_MAKE_SET
AC_PROG_RANLIB

AC_LANG([C])
# Checks for libraries.

# check for large file support
AC_SYS_LARGEFILE

# Checks for header files.
AC_CHECK_HEADERS([fcntl.h limits.h stdlib.h string.h sys/mount.h unistd.h])
AC_CHECK_HEADERS([sys/quota.h], [], [AC_MSG_ERROR(sys/quota.h unavailable)])
AC_CHECK_HEADERS([xfs/xqm.h], [], [AC_MSG_ERROR(xfs/xqm.h unavailable)])

# Checks for typedefs, structures, and compiler characteristics.
AC_CHECK_HEADER_STDBOOL
AC_TYPE_UID_T
AC_TYPE_MODE_T
AC_TYPE_PID_T
AC_TYPE_SIZE_T

# Checks for library functions.
AC_FUNC_CHOWN
AC_FUNC_ERROR_AT_LINE
AC_FUNC_FORK
AC_FUNC_STRNLEN
AC_CHECK_FUNCS([mkdir regcomp setenv strdup strerror secure_getenv])

AC_ARG_WITH([unit-tests],
    AC_HELP_STRING([--without-unit-tests], [do not build unit test programs]),
    [case "${withval}" in
        yes) with_unit_tests=yes ;;
        no)  with_unit_tests=no ;;
        *)   AC_MSG_ERROR([bad value ${withval} for --without-unit-tests])
    esac], [with_unit_tests=yes])
AM_CONDITIONAL([WITH_UNIT_TESTS], [test "x$with_unit_tests" = "xyes"])

# Allow to build without apparmor support by calling:
# ./configure --disable-apparmor
# This makes it possible to run snaps in devmode on almost any host,
# regardless of the kernel version.
AC_ARG_ENABLE([apparmor],
    AS_HELP_STRING([--disable-apparmor], [Disable apparmor support]),
    [case "${enableval}" in
        yes) enable_apparmor=yes ;;
        no)  enable_apparmor=no ;;
        *) AC_MSG_ERROR([bad value ${enableval} for --disable-apparmor])
    esac], [enable_apparmor=yes])
AM_CONDITIONAL([APPARMOR], [test "x$enable_apparmor" = "xyes"])

# Allow to build with SELinux support by calling:
# ./configure --enable-selinux
AC_ARG_ENABLE([selinux],
    AS_HELP_STRING([--enable-selinux], [Enable SELinux support]),
    [case "${enableval}" in
        yes) enable_selinux=yes ;;
        no)  enable_selinux=no ;;
        *) AC_MSG_ERROR([bad value ${enableval} for --enable-selinux])
    esac], [enable_selinux=no])
AM_CONDITIONAL([SELINUX], [test "x$enable_selinux" = "xyes"])

# Enable older tests only when confinement is enabled and we're building for PC
# The tests are of smaller value as we port more and more tests to spread.
AM_CONDITIONAL([CONFINEMENT_TESTS], [test "x$enable_apparmor" = "xyes" && ((test "x$host_cpu" = "xx86_64" && test "x$build_cpu" = "xx86_64") || (test "x$host_cpu" = "xi686" && test "x$build_cpu" = "xi686"))])

# Check for glib that we use for unit testing
AS_IF([test "x$with_unit_tests" = "xyes"], [
    PKG_CHECK_MODULES([GLIB], [glib-2.0])
])

# Check if apparmor userspace library is available.
AS_IF([test "x$enable_apparmor" = "xyes"], [
    # Expect AppArmor3 when building as a snap under snapcraft
    AS_IF([test "x$SNAPCRAFT_PROJECT_NAME" = "xsnapd"], [
        PKG_CHECK_MODULES([APPARMOR3], [libapparmor >= 3.0.4], [
            AC_DEFINE([HAVE_APPARMOR], [1], [Build with apparmor3 support])], [
            AC_MSG_ERROR([unable to find apparmor3 for snap build of snapd])])], [
        PKG_CHECK_MODULES([APPARMOR], [libapparmor], [
      AC_DEFINE([HAVE_APPARMOR], [1], [Build with apparmor support])])])
], [
    AC_MSG_WARN([
    XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
    X                                                     X
    X Apparmor is disabled, all snaps will run in devmode X
    X                                                     X
    XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX])
])

# Check if SELinux userspace library is available.
AS_IF([test "x$enable_selinux" = "xyes"], [
PKG_CHECK_MODULES([SELINUX], [libselinux], [
AC_DEFINE([HAVE_SELINUX], [1], [Build with SELinux support])])
])

# Check if udev and libudev are available.
# Those are now used unconditionally even if apparmor is disabled.
PKG_CHECK_MODULES([LIBUDEV], [libudev])
PKG_CHECK_MODULES([UDEV], [udev])

# Check if libcap is available.
# PKG_CHECK_MODULES([LIBCAP], [libcap])

# Enable special support for hosts with proprietary nvidia drivers on Ubuntu.
AC_ARG_ENABLE([nvidia-multiarch],
    AS_HELP_STRING([--enable-nvidia-multiarch], [Support for proprietary nvidia drivers (Ubuntu/Debian)]),
    [case "${enableval}" in
        yes) enable_nvidia_multiarch=yes ;;
        no)  enable_nvidia_multiarch=no ;;
        *) AC_MSG_ERROR([bad value ${enableval} for --enable-nvidia-multiarch])
    esac], [enable_nvidia_multiarch=no])
AM_CONDITIONAL([NVIDIA_MULTIARCH], [test "x$enable_nvidia_multiarch" = "xyes"])

AS_IF([test "x$enable_nvidia_multiarch" = "xyes"], [
    AC_DEFINE([NVIDIA_MULTIARCH], [1],
        [Support for proprietary nvidia drivers (Ubuntu/Debian)])])

# Enable special support for hosts with proprietary nvidia drivers on Arch.
AC_ARG_ENABLE([nvidia-biarch],
    AS_HELP_STRING([--enable-nvidia-biarch], [Support for proprietary nvidia drivers (bi-arch distributions)]),
    [case "${enableval}" in
        yes) enable_nvidia_biarch=yes ;;
        no)  enable_nvidia_biarch=no ;;
        *) AC_MSG_ERROR([bad value ${enableval} for --enable-nvidia-biarch])
    esac], [enable_nvidia_biarch=no])
AM_CONDITIONAL([NVIDIA_BIARCH], [test "x$enable_nvidia_biarch" = "xyes"])

AS_IF([test "x$enable_nvidia_biarch" = "xyes"], [
    AC_DEFINE([NVIDIA_BIARCH], [1],
        [Support for proprietary nvidia drivers (bi-arch distributions)])])

AC_ARG_ENABLE([merged-usr],
    AS_HELP_STRING([--enable-merged-usr], [Enable support for merged /usr directory]),
    [case "${enableval}" in
        yes) enable_merged_usr=yes ;;
        no)  enable_merged_usr=no ;;
        *) AC_MSG_ERROR([bad value ${enableval} for --enable-merged-usr])
    esac], [enable_merged_usr=no])
AM_CONDITIONAL([MERGED_USR], [test "x$enable_merged_usr" = "xyes"])

AS_IF([test "x$enable_merged_usr" = "xyes"], [
    AC_DEFINE([MERGED_USR], [1],
        [Support for merged /usr directory])])

SNAP_MOUNT_DIR="/snap"
AC_ARG_WITH([snap-mount-dir],
    AS_HELP_STRING([--with-snap-mount-dir=DIR], [Use an alternate snap mount directory]),
    [SNAP_MOUNT_DIR="$withval"])
AC_SUBST(SNAP_MOUNT_DIR)
AC_DEFINE_UNQUOTED([SNAP_MOUNT_DIR], "${SNAP_MOUNT_DIR}", [Location of the snap mount points])

SNAP_MOUNT_DIR_SYSTEMD_UNIT="$(systemd-escape -p "$SNAP_MOUNT_DIR")"
AC_SUBST([SNAP_MOUNT_DIR_SYSTEMD_UNIT])
AC_DEFINE_UNQUOTED([SNAP_MOUNT_DIR_SYSTEMD_UNIT], "${SNAP_MOUNT_DIR_SYSTEMD_UNIT}", [Systemd unit name for snap mount points location])

AC_PATH_PROGS([HAVE_RST2MAN],[rst2man rst2man.py])
AS_IF([test "x$HAVE_RST2MAN" = "x"], [AC_MSG_WARN(["cannot find the rst2man tool, install python-docutils or similar"])])
AM_CONDITIONAL([HAVE_RST2MAN], [test "x${HAVE_RST2MAN}" != "x"])

AC_PATH_PROG([HAVE_VALGRIND],[valgrind])
AM_CONDITIONAL([HAVE_VALGRIND], [test "x${HAVE_VALGRIND}" != "x"])
AS_IF([test "x$HAVE_VALGRIND" = "x"], [AC_MSG_WARN(["cannot find the valgrind tool, will not run unit tests through valgrind"])])

# Allow linking selected libraries statically for reexec.
AC_ARG_ENABLE([static-libcap],
    AS_HELP_STRING([--enable-static-libcap], [Link libcap statically]),
    [case "${enableval}" in
        yes) enable_static_libcap=yes ;;
        no)  enable_static_libcap=no ;;
        *) AC_MSG_ERROR([bad value ${enableval} for --enable-static-libcap])
    esac], [enable_static_libcap=no])
AM_CONDITIONAL([STATIC_LIBCAP], [test "x$enable_static_libcap" = "xyes"])

AC_ARG_ENABLE([static-libapparmor],
    AS_HELP_STRING([--enable-static-libapparmor], [Link libapparmor statically]),
    [case "${enableval}" in
        yes) enable_static_libapparmor=yes ;;
        no)  enable_static_libapparmor=no ;;
        *) AC_MSG_ERROR([bad value ${enableval} for --enable-static-libapparmor])
    esac], [enable_static_libapparmor=no])
AM_CONDITIONAL([STATIC_LIBAPPARMOR], [test "x$enable_static_libapparmor" = "xyes"])

AC_ARG_ENABLE([static-libselinux],
AS_HELP_STRING([--enable-static-libselinux], [Link libselinux statically]),
[case "${enableval}" in
yes) enable_static_libselinux=yes ;;
no)  enable_static_libselinux=no ;;
*) AC_MSG_ERROR([bad value ${enableval} for --enable-static-libselinux])
esac], [enable_static_libselinux=no])
AM_CONDITIONAL([STATIC_LIBSELINUX], [test "x$enable_static_libselinux" = "xyes"])

LIB32_DIR="${prefix}/lib32"
AC_ARG_WITH([32bit-libdir],
    AS_HELP_STRING([--with-32bit-libdir=DIR], [Use an alternate lib32 directory]),
    [LIB32_DIR="$withval"])
AC_SUBST(LIB32_DIR)
AC_DEFINE_UNQUOTED([LIB32_DIR], "${LIB32_DIR}", [Location of the lib32 directory])

AC_ARG_WITH([host-arch-triplet],
    AS_HELP_STRING([--with-host-arch-triplet=triplet], [Arch triplet for host libraries]),
    [HOST_ARCH_TRIPLET="$withval"])
AC_SUBST(HOST_ARCH_TRIPLET)
AC_DEFINE_UNQUOTED([HOST_ARCH_TRIPLET], "${HOST_ARCH_TRIPLET}", [Arch triplet for host libraries])

AC_ARG_WITH([host-arch-32bit-triplet],
    AS_HELP_STRING([--with-host-arch-32bit-triplet=triplet], [Arch triplet for 32bit libraries]),
    [HOST_ARCH32_TRIPLET="$withval"])
AC_SUBST(HOST_ARCH32_TRIPLET)
AC_DEFINE_UNQUOTED([HOST_ARCH32_TRIPLET], "${HOST_ARCH32_TRIPLET}", [Arch triplet for 32bit libraries])

SYSTEMD_SYSTEM_GENERATOR_DIR="$($PKG_CONFIG --variable=systemdsystemgeneratordir systemd)"
AS_IF([test "x$SYSTEMD_SYSTEM_GENERATOR_DIR" = "x"], [SYSTEMD_SYSTEM_GENERATOR_DIR=/lib/systemd/system-generators])
AC_SUBST(SYSTEMD_SYSTEM_GENERATOR_DIR)

# FIXME: get this via something like pkgconf once it is defined there
SYSTEMD_SYSTEM_ENV_GENERATOR_DIR="${prefix}/lib/systemd/system-environment-generators"
AC_SUBST(SYSTEMD_SYSTEM_ENV_GENERATOR_DIR)

AC_ARG_ENABLE([bpf],
AS_HELP_STRING([--enable-bpf], [Enable BPF support]),
[case "${enableval}" in
yes) enable_bpf=yes ;;
no)  enable_bpf=no ;;
*) AC_MSG_ERROR([bad value ${enableval} for --enable-bpf])
esac],
[enable_bpf=yes])
AM_CONDITIONAL([ENABLE_BPF], [test "x$enable_bpf" = "xyes"])

AS_IF([test "x$enable_bpf" = "xyes"], [
  AC_DEFINE([ENABLE_BPF], [1], [Enable BPF support])

  AC_CACHE_CHECK([whether host BPF headers are usable], [snapd_cv_bpf_header_works], [
      AC_COMPILE_IFELSE(
        [AC_LANG_SOURCE([[
#include <linux/bpf.h>
void foo(enum bpf_attach_type type) {}
void bar() { struct bpf_cgroup_dev_ctx ctx = {0}; }
]])],
          [snapd_cv_bpf_header_works=yes],
          [snapd_cv_bpf_header_works=no])
  ])

  AS_IF([test "x$snapd_cv_bpf_header_works" = "xno"], [
    use_internal_pbf_headers=yes
  ])
], [
  use_internal_pbf_headers=no
])
AM_CONDITIONAL([USE_INTERNAL_BPF_HEADERS], [test "x$use_internal_pbf_headers" = "xyes"])

AC_CACHE_CHECK([whether -Wmissing-field-initializers is correct], [snapd_cv_missing_field_initializers_works], [
  save_CFLAGS="${CFLAGS}"
  CFLAGS="${CFLAGS} -Wmissing-field-initializers -Werror"
  AC_COMPILE_IFELSE(
    [AC_LANG_SOURCE([[
struct { int a; int b; } a = { 0 };
struct { const char* a; int b; } b[] = { {.a = ""}, {} };
]])], [
    snapd_cv_missing_field_initializers_works=yes
  ], [
    snapd_cv_missing_field_initializers_works=no
  ])
  CFLAGS="${save_CFLAGS}"
])

AX_APPEND_COMPILE_FLAGS([ dnl
  -Wall dnl
  -Wextra dnl
  -Wmissing-prototypes dnl
  -Wstrict-prototypes dnl
  -Wno-unused-parameter dnl
 ], [CHECK_CFLAGS])

AS_IF([test "x$snapd_cv_missing_field_initializers_works" = "xno"], [
  AX_APPEND_COMPILE_FLAGS([-Wno-missing-field-initializers], [CHECK_CFLAGS])
])

AS_IF([test "x$with_unit_tests" = "xyes"], [
  AX_APPEND_COMPILE_FLAGS([-Werror], [CHECK_CFLAGS])
])

AC_CONFIG_FILES([Makefile])
AC_OUTPUT
