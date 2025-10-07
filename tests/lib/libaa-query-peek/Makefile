prefix ?= /usr/local
libdir ?= $(prefix)/lib
DESTDIR ?=
CFLAGS = -Wall -Werror
CPPFLAGS ?=

INSTALL ?= install
RM ?= rm

n = libaa-query-peek.so

.PHONY: all clean install uninstall
all: $(n)
clean:
	$(RM) $(n)
install: $(DESTDIR)$(libdir)/$(n)
uninstall:
	$(RM) $(DESTDIR)$(libdir)/$(n)
$(DESTDIR)$(libdir)/$(n): $(n)
	$(INSTALL) -m 755 $< $@
$(n): CFLAGS += -fPIC -shared
$(n): LDLIBS += -ldl
$(n): query.c
	$(CC) $(OUTPUT_OPTION) $(CFLAGS) $(CPPFLAGS) $^ $(LDLIBS)
