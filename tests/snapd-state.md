# The snapd state

Snapd state is saved during the project prepare and it is restored when any test is prepared to make sure the initial state is the same for all the tests executed on the snapd test suite. 

The snapd state is dropped in this "tests/" directory and restored from there. For classic systems the snapd state is saved in a single file "$SPREAD_PATH/tests/snapd-state.tar" and for all-snaps systems it is saved in a "$SPREAD_PATH/tests/snapd-state" directory which is done to get better performance on boards.
