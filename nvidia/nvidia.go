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

// ExceptionGlobs match libraries present in the driver package that are not expected to be used in snaps.
//
// In practice those are libraries loaded from the X11 server.
var ExceptionGlobs = []string{
	"nvidia/xorg/libglx.so*",
	"nvidia/xorg/libglxserver_nvidia.so*",
	"nvidia/xorg/nvidia_drv.so*",
}

// RegularGlobs match libraries present in the driver package that may be used in snaps.
var RegularGlobs = []string{
	"gbm/nvidia-drm_gbm.so*",
	"libEGL_nvidia.so*",
	"libGLESv1_CM_nvidia.so*",
	"libGLESv2_nvidia.so*",
	"libGLX_nvidia.so*",
	"libXvMCNVIDIA.so*",
	"libXvMCNVIDIA_dynamic.so*",
	"libcublas.so*",
	"libcublasLt.so*",
	"libcuda.so*",
	"libcudadebugger.so*",
	"libcudart.so*",
	"libcudnn.so*",
	"libcudnn_adv_infer*",
	"libcudnn_adv_train*",
	"libcudnn_cnn_infer*",
	"libcudnn_cnn_train*",
	"libcudnn_ops_infer*",
	"libcudnn_ops_train*",
	"libcufft.so*",
	"libcuparse.so*",
	"libcurand.so*",
	"libcusolver.so*",
	"libnppc.so*",
	"libnppcif.so*",
	"libnppial.so*",
	"libnppicc.so*",
	"libnppidei.so*",
	"libnppig.so*",
	"libnppim.so*",
	"libnppist.so*",
	"libnppitc.so*",
	"libnvToolsExt.so*",
	"libnvcuvid.so*",
	"libnvdc.so*",
	"libnvidia-allocator.so*",
	"libnvidia-api.so*",
	"libnvidia-cbl.so*",
	"libnvidia-cfg.so*",
	"libnvidia-cfg.so*",
	"libnvidia-compiler-next.so*",
	"libnvidia-compiler.so*",
	"libnvidia-egl-gbm.so*",
	"libnvidia-egl-wayland*",
	"libnvidia-eglcore.so*",
	"libnvidia-encode.so*",
	"libnvidia-fatbinaryloader.so*",
	"libnvidia-fbc.so*",
	"libnvidia-glcore.so*",
	"libnvidia-glsi.so*",
	"libnvidia-glvkspirv.so*",
	"libnvidia-gpucomp.so*",
	"libnvidia-ifr.so*",
	"libnvidia-ml.so*",
	"libnvidia-ngx.so*",
	"libnvidia-nscq.so*",
	"libnvidia-nvvm.so*",
	"libnvidia-opencl.so*",
	"libnvidia-opticalflow.so*",
	"libnvidia-pkcs11-openssl3.so*",
	"libnvidia-pkcs11.so*",
	"libnvidia-ptxjitcompiler.so*",
	"libnvidia-rtcore.so*",
	"libnvidia-tls.so*",
	"libnvidia-vulkan-producer.so*",
	"libnvimp.so*",
	"libnvoptix.so*",
	"libnvos.so*",
	"libnvrm.so*",
	"libnvrm_gpu.so*",
	"libnvrm_graphics.so*",
	"libnvrtc*",
	"libnvrtc-builtins*",
	"tls/libnvidia-tls.so*",
	"vdpau/libvdpau_nvidia.so*",
}
