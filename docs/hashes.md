# The hashes.yaml file

The meta/hashes.yaml describes each file in a snap packages and the
compressed tar that contains these files.

## Format

The yaml looks like this:
* archive-sha512: the hexdigest of the data.tar.gz

Then a list of files/directories/symlinks in the tar that are
described with:
* name: the full path in the archive
* mode: First char is the type "f" for file, "d" for dir, "l" for
        symlink. Then the unix permission bits in the form
        rwxrwxrwx.
        E.g. a file with mode 0644 is: "frw-r--r--"
* size: (applies only to files)
* sha512: (applies only to files) the hexdigest of the file content

## Owner

There is no owner in the format currently, a snap package will always
be unpacked to a static non-root owner regardless what owner it has in
the data.tar.gz.


# Future
In the future "xattr" will be supported.
  
