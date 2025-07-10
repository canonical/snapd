#!/usr/bin/awk -f
# This awk program rewrites peer group IDs, as described by
# proc_pid_mountinfo(5), into sequential values starting from the magic value
# 42.

BEGIN {
    # Start with 42 as a recognizable group number.
    group = 42
    # Conveniently both "shared" and "master" have the same length.
    # This also makes the match below easier as there's only one regular
    # expression and one loop to work with.
    prefix1_len = length("shared:")
    # The second prefix is for the less commonly seen "propagate_from" field.
    prefix2_len = length("propagate_from:")
}

{
    # Starting with field 7, which is the first optional field, rewrite
    # subsequent fields until we reach the end all fields or until we see the
    # optional field list terminator, dash.
    for (n = 7; n < NF && $(n) != "-"; n++) {
        # Find the NNN number of the peer group ID.
        if (match($(n), /(shared|master):[0-9]+/)) {
            id = substr($(n), RSTART + prefix1_len)
        } else if (match($(n), /propagate_from:[0-9]+/)) {
            id = substr($(n), RSTART + prefix2_len)
        } else {
            continue
        }

        # If we have not seen this number yet, assign the next replacement ID.
        if (!(id in seen)) {
            seen[id] = group++
        }

        # Replace the actual peer group ID with the replacement ID.
        sub(/[0-9]+/, seen[id], $(n))
    }

    # Print each rewritten record.
    print
}
