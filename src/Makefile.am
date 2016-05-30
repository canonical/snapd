bin_PROGRAMS = snap-run
snap_run_SOURCES = main.c seccomp_utils.c utils.c
snap_run_CFLAGS = \
    $(AM_CFLAGS) \
    $(SECCOMP_CFLAGS) \
    $(APPARMOR_CFLAGS) \
    $(UDEV_CFLAGS)
snap_run_LDADD = \
    $(SECCOMP_LIBS) \
    $(APPARMOR_LIBS) \
    $(UDEV_LIBS)