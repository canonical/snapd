# Files list

`OVMF_VARS.test.fd` custom OVMF variables block content with enrolled PK, KEK
and Snakeoil keys. Used for dbx testing.

Matches the following code block:

```
2b372dd411e38931fb12e80834cbb7ff54b3d36b6c2c63629ab1a569a1d87f1b  OVMF_CODE.secboot.fd
```

# Generate a new data set

``` sh

# grab snakeoil keys
cp PkKek-1-snakeoil.pem PkKek-1-snakeoil.crt

make clean
make clean-disk

# generate data
# signing keys: sign.crt and the snakeoil key
# initial dbx blacklists bogus.crt, and an update which blacklists other-bogus.crt
make dataset SIGNING_KEYS='sign.crt PkKek-1-snakeoil.crt' BLACKLIST_KEYS='bogus.crt'

# prepare disk for enrolling keys
make tree/db.auth
make tree/dbx-blacklist.auth
make disk.img
```


# QEMU

``` sh
cp OVMF_VARS.fd OVMF_VARS.test.fd

# run without snapshot mode
qemu-system-x86_64 -enable-kvm -smp 4 -m 2048 -cpu host -machine q35 -global ICH9-LPC.disable_s3=1 \
        -drive file=./OVMF_CODE.secboot.fd,if=pflash,format=raw,unit=0,readonly=on \
        -drive file=./OVMF_VARS.test.fd,if=pflash,format=raw \
        -drive file=efi-auth/disk.img,if=none,id=disk1,snapshot=on \
        -device virtio-blk-pci,drive=disk1,bootindex=1 
        
# device configuration -> secure boot -> custom
# 1. enroll PK
# 2. enroll KEK
# 3. enroll DB
# 4. enroll DBX?

# reset
# poweroff
```


# EFI

## efitools

Initial check:

``` sh
$ sudo efitools.tool efi-readvar   
Variable PK, length 811                                   
PK: List 0, type X509                                           
    Signature 0, size 783, owner 8be4df61-93ca-11d2-aa0d-00e098032b8c
        Subject:                            
            CN=PK                                                      
        Issuer:                                                      
            CN=PK                                           
Variable KEK, length 813
KEK: List 0, type X509                                      
    Signature 0, size 785, owner 00000000-0000-0000-0000-000000000000
        Subject:                                                                                                                                                                               
            CN=KEK               
        Issuer:
            CN=KEK
Variable db, length 1640
db: List 0, type X509
    Signature 0, size 787, owner 11111111-0000-1111-0000-123456789abc
        Subject:
            CN=sign
        Issuer:
            CN=sign
db: List 1, type X509
    Signature 0, size 797, owner 11111111-0000-2222-0000-000000000000
        Subject:
            O=Snake Oil
        Issuer:
            O=Snake Oil
Variable dbx, length 817
dbx: List 0, type X509
    Signature 0, size 789, owner 11111111-0000-1111-0000-123456789abc
        Subject:
            CN=bogus
        Issuer:
            CN=bogus
Variable MokList has no entries
```

Manually importing from `*.auth` files:

``` sh
root@localhost:/home/maciek-borzecki# efitools.tool efi-updatevar -f PK.auth PK
root@localhost:/home/maciek-borzecki# efitools.tool efi-updatevar -f KEK.auth KEK
root@localhost:/home/maciek-borzecki# efitools.tool efi-updatevar -f db.auth db
root@localhost:/home/maciek-borzecki# efitools.tool efi-updatevar -f dbx-blacklist.auth

# or subsequent update
root@localhost:/home/maciek-borzecki# efitools.tool efi-updatevar -a -f dbx-update-blacklist.auth
```

Variables may be marked as immutable, switch them to mutable (and maybe switch
back to immutable later):

``` sh
chattr -i /sys/firmware/efi/efivars/{PK,KEK,db,dbx}-*
```

To subsequently clear everything, use `empty.auth` generated in the data set:

``` sh
root@localhost:/home/maciek-borzecki# efitools.tool efi-updatevar -f empty.auth PK
root@localhost:/home/maciek-borzecki# efitools.tool efi-updatevar -f empty.auth KEK
root@localhost:/home/maciek-borzecki# efitools.tool efi-updatevar -f empty.auth db
root@localhost:/home/maciek-borzecki# efitools.tool efi-updatevar -f empty.auth dbx
```

### quirks/notes

- crucial difference if payload is built for append or write
- cannot import an empty esl to dbx (?)

## Links

- LVFS metadata https://lvfs.readthedocs.io/en/latest/metainfo.html
- LVFS uploading firmware https://lvfs.readthedocs.io/en/latest/upload.html
- fwupd remotes https://github.com/fwupd/fwupd/blob/main/data/remotes.d/README.md
- useful notes: https://www.rodsbooks.com/efi-bootloaders/controlling-sb.html
- even more useful notes: https://docs.slackware.com/howtos:security:enabling_secure_boot

