# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: Zygmunt Krynicki

prefix ?= /usr/local
exec_prefix ?= $(prefix)
bindir ?= $(exec_prefix)/bin

.PHONY: all
all: plz-run
plz-run: GO ?= $(or $(shell which go),$(error program go is required))
plz-run: export CGO_ENABLED=0
plz-run: main.go
	$(GO) build -o $@

.PHONY: clean
clean:
	$(RM) -f plz-run

.PHONY: install
install:: plz-run $(if $(DESTDIR),| $(DESTDIR))
	install -m 755 -d $(DESTDIR)$(bindir)
	install -m 755 -t $(DESTDIR)$(bindir) plz-run

ifneq (,$(DESTDIR))
$(DESTDIR):
	mkdir -p $(DESTDIR)
endif

.PHONY: fmt fmt-go
fmt: fmt-go

fmt-go: GOFMT ?= $(or $(shell which gofmt),$(error program gofmt is required))
fmt-go: $(wildcard *.go)
	$(GOFMT) -s -w $^
