all:
	cp "$(SNAPCRAFT_PART_INSTALL)"/usr/lib/shim/shimx64.efi.dualsigned shim.efi.signed
	cp "$(SNAPCRAFT_PART_INSTALL)"/usr/lib/grub/x86_64-efi-signed/grubx64.efi.signed grubx64.efi

install:
	install -m 644 shim.efi.signed grubx64.efi $(DESTDIR)/
	install -m 644 grub.conf $(DESTDIR)/
	install -d $(DESTDIR)/meta
	install -m 644 gadget.yaml $(DESTDIR)/meta/