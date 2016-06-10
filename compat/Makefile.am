dist_bin_SCRIPTS = ubuntu-core-launcher
CLEANFILES = $(dist_bin_SCRIPTS)
EXTRA_DIST = ubuntu-core-launcher.in

ubuntu-core-launcher: ubuntu-core-launcher.in Makefile
	sed -e 's,[@]libdir[@],$(libdir),g' < $(srcdir)/ubuntu-core-launcher.in > ubuntu-core-launcher
	chmod +x ubuntu-core-launcher
