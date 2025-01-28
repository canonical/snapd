/*
 * Copyright (C) 2016 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

#ifndef SNAP_CONFINE_UNIT_TESTS_H
#define SNAP_CONFINE_UNIT_TESTS_H

/**
 * Run unit tests and exit.
 *
 * The function inspects and modifies command line arguments.
 * Internally it is using glib-test functions.
 */
int sc_run_unit_tests(int *argc, char ***argv);

#endif  // SNAP_CONFINE_SANITY_H
