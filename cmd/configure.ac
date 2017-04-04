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
AC_CHECK_HEADERS([xfs/xqm.h], [], [AC_MSG_ERROR(xfs/xqm.h unavailable)],
[[#define _FILE_OFFSET_BITS 64
#include <xfs/xqm.h>
]])

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

# Allow to build without seccomp support by calling:
# ./configure --disable-seccomp
# This is separate because seccomp support is generally very good and it
# provides useful confinement for unsafe system calls.
AC_ARG_ENABLE([seccomp],
    AS_HELP_STRING([--disable-seccomp], [Disable seccomp support]),
    [case "${enableval}" in
        yes) enable_seccomp=yes ;;
        no)  enable_seccomp=no ;;
        *) AC_MSG_ERROR([bad value ${enableval} for --disable-seccomp])
    esac], [enable_seccomp=yes])
AM_CONDITIONAL([SECCOMP], [test "x$enable_seccomp" = "xyes"])

# Enable older tests only when confinement is enabled and we're building for PC
# The tests are of smaller value as we port more and more tests to spread.
AM_CONDITIONAL([CONFINEMENT_TESTS], [test "x$enable_apparmor" = "xyes" && test "x$enable_seccomp" = "xyes" && ((test "x$host_cpu" = "xx86_64" && test "x$build_cpu" = "xx86_64") || (test "x$host_cpu" = "xi686" && test "x$build_cpu" = "xi686"))])

# Check for glib that we use for unit testing
AS_IF([test "x$with_unit_tests" = "xyes"], [
    PKG_CHECK_MODULES([GLIB], [glib-2.0])
])

# Check if seccomp userspace library is available
AS_IF([test "x$enable_seccomp" = "xyes"], [
    PKG_CHECK_MODULES([SECCOMP], [libseccomp], [
      AC_DEFINE([HAVE_SECCOMP], [1], [Build with seccomp support])])
])

# Check if apparmor userspace library is available.
AS_IF([test "x$enable_apparmor" = "xyes"], [
    PKG_CHECK_MODULES([APPARMOR], [libapparmor], [
      AC_DEFINE([HAVE_APPARMOR], [1], [Build with apparmor support])])
], [
    AC_MSG_WARN([
    XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
    X                                                     X
    X Apparmor is disabled, all snaps will run in devmode X
    X                                                     X
    XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX])
])

# Check if udev and libudev are available.
# Those are now used unconditionally even if apparmor is disabled.
PKG_CHECK_MODULES([LIBUDEV], [libudev])
PKG_CHECK_MODULES([UDEV], [udev])

# Check if libcap is available.
# PKG_CHECK_MODULES([LIBCAP], [libcap])

# Enable special support for hosts with proprietary nvidia drivers on Ubuntu.
AC_ARG_ENABLE([nvidia-ubuntu],
    AS_HELP_STRING([--enable-nvidia-ubuntu], [Support for proprietary nvidia drivers (Ubuntu)]),
    [case "${enableval}" in
        yes) enable_nvidia_ubuntu=yes ;;
        no)  enable_nvidia_ubuntu=no ;;
        *) AC_MSG_ERROR([bad value ${enableval} for --enable-nvidia-ubuntu])
    esac], [enable_nvidia_ubuntu=no])
AM_CONDITIONAL([NVIDIA_UBUNTU], [test "x$enable_nvidia_ubuntu" = "xyes"])

AS_IF([test "x$enable_nvidia_ubuntu" = "xyes"], [
    AC_DEFINE([NVIDIA_UBUNTU], [1],
        [Support for proprietary nvidia drivers (Ubuntu)])])

# Enable special support for hosts with proprietary nvidia drivers on Arch.
AC_ARG_ENABLE([nvidia-arch],
    AS_HELP_STRING([--enable-nvidia-arch], [Support for proprietary nvidia drivers (Arch)]),
    [case "${enableval}" in
        yes) enable_nvidia_arch=yes ;;
        no)  enable_nvidia_arch=no ;;
        *) AC_MSG_ERROR([bad value ${enableval} for --enable-nvidia-arch])
    esac], [enable_nvidia_arch=no])
AM_CONDITIONAL([NVIDIA_ARCH], [test "x$enable_nvidia_arch" = "xyes"])

AS_IF([test "x$enable_nvidia_arch" = "xyes"], [
    AC_DEFINE([NVIDIA_ARCH], [1],
        [Support for proprietary nvidia drivers (Arch)])])

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

AC_ARG_ENABLE([caps-over-setuid],
    AS_HELP_STRING([--enable-caps-over-setuid], [Use capabilities rather than setuid bit]),
    [case "${enableval}" in
        yes) enable_caps_over_setuid=yes ;;
        no)  enable_caps_over_setuid=no ;;
        *) AC_MSG_ERROR([bad value ${enableval} for --enable-caps-over-setuid])
    esac], [enable_caps_over_setuid=no])
AM_CONDITIONAL([CAPS_OVER_SETUID], [test "x$enable_caps_over_setuid" = "xyes"])

AS_IF([test "x$enable_caps_over_setuid" = "xyes"], [
    AC_DEFINE([CAPS_OVER_SETUID], [1],
        [Use capabilities rather than setuid bit])])

AC_PATH_PROGS([HAVE_RST2MAN],[rst2man rst2man.py])
AS_IF([test "x$HAVE_RST2MAN" = "x"], [AC_MSG_ERROR(["cannot find the rst2man tool, install python-docutils or similar"])])

AC_PATH_PROG([HAVE_SHELLCHECK],[shellcheck])
AM_CONDITIONAL([HAVE_SHELLCHECK], [test "x${HAVE_SHELLCHECK}" != "x"])
AS_IF([test "x$HAVE_SHELLCHECK" = "x"], [AC_MSG_WARN(["cannot find the shellcheck tool, will not run syntax checks for shell code"])])

AC_PATH_PROG([HAVE_VALGRIND],[valgrind])
AM_CONDITIONAL([HAVE_VALGRIND], [test "x${HAVE_VALGRIND}" != "x"])
AS_IF([test "x$HAVE_VALGRIND" = "x"], [AC_MSG_WARN(["cannot find the valgrind tool, will not run unit tests through valgrind"])])

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

AC_ARG_ENABLE([static-libseccomp],
    AS_HELP_STRING([--enable-static-libseccomp], [Link libseccomp statically]),
    [case "${enableval}" in
        yes) enable_static_libseccomp=yes ;;
        no)  enable_static_libseccomp=no ;;
        *) AC_MSG_ERROR([bad value ${enableval} for --enable-static-libseccomp])
    esac], [enable_static_libseccomp=no])
AM_CONDITIONAL([STATIC_LIBSECCOMP], [test "x$enable_static_libseccomp" = "xyes"])

AC_CONFIG_FILES([Makefile])
AC_OUTPUT
