dist_man_MANS = snap-confine.5 ubuntu-core-launcher.1

CLEANFILES = snap-confine.5 ubuntu-core-launcher.1
EXTRA_DIST = snap-confine.rst ubuntu-core-launcher.rst

%.5: %.rst
	$(HAVE_RST2MAN) $^ > $@

ubuntu-core-launcher.1: ubuntu-core-launcher.rst
	$(HAVE_RST2MAN) $^ > $@
