## Autopkgtest

In order to run the autopkgtest suite locally you need first to generate an image:

    $ adt-buildvm-ubuntu-cloud -a amd64 -r xenial -v

This will create a `adt-xenial-amd64-cloud.img` file, then you can run the tests from
the project's root with:

    $ adt-run --unbuilt-tree . --- qemu ./adt-xenial-amd64-cloud.img
