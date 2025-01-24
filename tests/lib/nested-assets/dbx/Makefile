##
# Project Title
#
# @file
# @version 0.1

# from https://git.kernel.org/pub/scm/linux/kernel/git/jejb/efitools.git/tree/Make.rules
# also: https://gist.github.com/rgl/fd1104c9d63843def5a111863df99898
#
MYGUID = 11111111-0000-1111-0000-000000000000

SIGNING_KEYS = sign.crt
BLACKLIST_KEYS = bogus.crt

%.crt:
	openssl req -new -x509 -newkey rsa:2048 -subj "/CN=$*/" -keyout $*.key -out $@ -days 3650 -nodes -sha256

%.esl: %.crt
	cert-to-efi-sig-list -g $(MYGUID) $< $@

# %-blacklist.esl: %.crt
# 	cert-to-efi-sig-list -g $(MYGUID) $< $@

%.cer: %.crt
	openssl x509 -in $< -out $@ -outform der

getcert = $(shell if [ "$(1)" = "PK" -o "$(1)" = "KEK" ]; then echo "-c PK.crt -k PK.key"; else echo "-c KEK.crt -k KEK.key"; fi)
getvar = $(shell if [ "$(1)" = "PK" -o "$(1)" = "KEK" ]; then echo $(1); else echo db; fi)

%.auth: %.esl PK.crt KEK.crt
	sign-efi-sig-list $(call getcert,$*) $(call getvar,$*) $< $@

%-update.auth: %.esl PK.crt KEK.crt
	sign-efi-sig-list -a $(call getcert,$*) $(call getvar,$*) $< $@

%-blacklist.auth: %-blacklist.esl KEK.crt
	sign-efi-sig-list -c KEK.crt -k KEK.key dbx $< $@

%-update-blacklist.auth: %-update-blacklist.esl KEK.crt
	sign-efi-sig-list -a -c KEK.crt -k KEK.key dbx $< $@

db.esl: $(patsubst %.crt,%.esl,$(SIGNING_KEYS))
	cat $^ > $@

%-blacklist.esl: $(patsubst %.crt,%.esl,$(BLACKLIST_KEYS))
	cat $^ > $@

tree/%: %
	mkdir -p $$(dirname $@)
	cp -av $< $@

clean-disk:
	rm -rfv ./tree
	rm -fv disk.img

clean-certs:
	rm -fv PK.cer PK.crt
	rm -fv KEK.cer KEK.crt

disk.img: tree/PK.cer tree/KEK.cer ./repart.d/*.conf
	rm -f disk.img
	systemd-repart \
		--empty=create \
		--size=auto \
		--copy-source=./tree/ \
		--offline=true \
		--definitions=./repart.d/ \
		disk.img

empty.auth: PK.crt
	rm -f empty
	touch empty
	sign-efi-sig-list -c PK.crt -k PK.key PK empty $@

dataset: PK.auth KEK.auth db.auth dbx-blacklist.auth empty.auth
	$(MAKE) dbx-1-update-blacklist.auth BLACKLIST_KEYS='$(BLACKLIST_KEYS) other-bogus.crt'

clean:
	rm -fv *.auth
	rm -fv *.esl

.PHONY: clean-disk clean-certs clean dataset
.PRECIOUS: %.esl %.cer %.crt %.auth %-blacklist.esl

# end
