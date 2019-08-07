name=lp-1812973

CFLAGS=-Wall -Werror

.PHONY: all
all: $(name)

.PHONY: clean
clean:
	rm -f $(name)

DESTDIR ?=

.PHONY: install
install: $(name)
	install -m 755 -d $(DESTDIR)/usr/bin/
	install -m 755 $(name) $(DESTDIR)/usr/bin/

.PHONY: fmt
fmt: $(name).c
	# NOTE: clang-format is using project-wide .clang-format file.
	clang-format -i $^
