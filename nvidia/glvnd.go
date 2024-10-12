// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package nvidia

// LibGlvndGlobs are related to libglvnd library.
//
// The library is further described at https://github.com/NVIDIA/libglvnd.
var LibGlvndGlobs = []string{
	"libEGL.so*",          // OpenGL for EGL window system.
	"libGLX_indirect.so*", // OpenGL for X11 window system, with indirect rendering.
	"libGLX.so*",          // OpenGL for X11 window system, with direct rendering.
	"libGL.so*",           // Wrapper around libGldispatch and libGLX.
	"libOpenGL.so*",       // Wrapper for libGLdispatch exposing openGL.
	"libGLESv1_CM.so*",    // Wrapper for libGLdispatch exposing openGL ES v1 (common profile).
	"libGLESv2.so*",       // Wrapper for libGLdispatch exposing openGL ES v2.
	"libGLdispatch.so*",   // Pass-through dispatching calls to vendor-specific library based on EGL or GLX rendering context.
	"libGLU.so*",          // OpenGL utility library.
}
