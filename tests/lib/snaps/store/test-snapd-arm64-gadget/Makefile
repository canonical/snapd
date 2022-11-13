all:
	cp $(SNAPCRAFT_PART_INSTALL)/usr/lib/shim/shimaa64.efi.dualsigned shim.efi.signed
	cp $(SNAPCRAFT_PART_INSTALL)/usr/lib/grub/arm64-efi-signed/grubaa64.efi.signed grubaa64.efi
	touch grub.conf

install:
	install -m 644 shim.efi.signed grubaa64.efi $(DESTDIR)/
	install -m 644 grub.conf $(DESTDIR)/
	install -d $(DESTDIR)/meta
	install -m 644 gadget.yaml $(DESTDIR)/meta/
