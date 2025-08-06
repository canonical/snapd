#!/usr/bin/awk -f
# This awk program consumes proc_pid_mountinfo(5) and displays a portion of the
# mount point and all the optional fields, up until the optional field
# separator, dash. The common prefix of the mount point and the current working
# directory is discarded.

BEGIN {
    prefix_len = length(ENVIRON["PWD"])
}

{
    # We will be printing fields one by one so use space for output record
    # separator and the empty string for output field separator. Each print
    # is one record.
    ORS = " "
    OFS = ""
    print $4
    # If the mount point starts with the current working directory then discard
    # the common prefix.  This makes test output more invariant to test
    # location or relocation.
    print substr($5, 1, prefix_len) == ENVIRON["PWD"] ? substr($5, prefix_len + 1) : $5

    # Starting with field 7, which is the first optional field, print
    # subsequent fields until we reach the end all fields or until we see the
    # optional field list terminator, dash.
    for (n=7; n<NF; n++) {
        # Ensure that the dash is printed too.
        print $(n)

        if ($(n) == "-") {
            break
        }

    }

    # Print the mount source with just the trailing newline.
    ORS = "\n"
    OFS = ""
    print $(n+2)
}
