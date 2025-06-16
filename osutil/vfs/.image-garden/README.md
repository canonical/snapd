# Image Garden State Directory

This directory holds temporary files created by image-garden. Here you may find
disk images (.qcow2), shell scripts to start particular virtual machines
(.run), log files from booting or from particular test runs as well as lock
files.

This directory may grow large but you may safely remove all the files present
therein with `git clean -xdf .image-garden`. The only consequence is that on
the next iteration of spread, the required files will be re-created.
