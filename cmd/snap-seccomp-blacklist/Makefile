.PHONY: all
all: $(foreach v,LE BE,$v-blacklist.bpf) | analyze

.PHONY: clean
clean:
	rm -f snap-seccomp-blacklist snap-seccomp-blacklist.o *.pfc *.bpf

.PHONY: fmt
fmt: snap-seccomp-blacklist.c
	clang-format -style='{BasedOnStyle: Google, IndentWidth: 4, ColumnLimit: 120}' -i $^

DESTDIR ?=

.PHONY: install
install:: blacklist.bpf | $(DESTDIR)/var/lib/snapd/seccomp/bpf
	install -m 644 $^ $|/global.bin

$(DESTDIR)/var/lib/snapd/seccomp/bpf:
	install -m 755 -d $@

$(foreach v,LE BE,$v-blacklist.pfc $v-blacklist.bpf): snap-seccomp-blacklist
	./$<

.PHONY: analyze
analyze: $(foreach v,LE BE,$v-blacklist.bpf $v-blacklist.pfc $v-bpf-script)
	# Not everyone has bpf_dbg installed, not everyone has support for "load bpf-blob".
	-bpf_dbg LE-bpf-script
	cat LE-blacklist.pfc
	-bpf_dbg BE-bpf-script
	cat BE-blacklist.pfc

snap-seccomp-blacklist: snap-seccomp-blacklist.o
	$(CC) -o $@ $^ $(LDLIBS)

%.o: %.c
	$(CC) -o $@ -c $^ $(CFLAGS)

CFLAGS += -Wall -Werror
CFLAGS += $(shell pkg-config libseccomp --cflags)
LDLIBS += $(shell pkg-config libseccomp --libs)
