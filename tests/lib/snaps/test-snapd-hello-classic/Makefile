test-snapd-hello-classic.bin: test-snapd-hello-classic.c
	$(CC) $(CFLAGS) $(LDFLAGS) -o $@ $<

install: test-snapd-hello-classic.bin
	install -D -t $(DESTDIR)/bin -m 0755 test-snapd-hello-classic.bin

.PHONY: clean
clean:
	rm -f test-snapd-hello-classic.bin
