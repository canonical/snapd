package builtin

const cudaSupportConnectedPlugAppArmor = `
# Description: CUDA requires being able to read/write to some nvidia devices

@{PROC}/sys/vm/mmap_min_addr r,
@{PROC}/devices r,
@{PROC}/modules r,
@{PROC}/driver/nvidia/params r,
/sys/devices/system/memory/block_size_bytes r,
unix (bind,listen) type=seqpacket addr="@cuda-uvmfd-[0-9a-f]*",
/dev/nvidia-uvm wr,
/dev/nvidiactl wr,
/{dev,run}/shm/cuda.* rw,
/var/lib/snapd/hostfs/{,usr/}lib{,32,64,x32}/{,@{multiarch}/}libcuda*.so{,.*} rm, 
`

const cudaSupportConnectedPlugSecComp = `
# Description: allow running operations on GPU devices
# necessary as cuda tries to create /dev/nvidia-uvm-tools with mknod
mknod - |S_IFCHR
`

const cudaSupportSummary = `allows access to NVIDIA devices using CUDA`

func init() {
	registerIface(&commonInterface{
		name:                  "cuda-support",
		summary:               cudaSupportSummary,
		connectedPlugAppArmor: cudaSupportConnectedPlugAppArmor,
		connectedPlugSecComp:  cudaSupportConnectedPlugSecComp,
		implicitOnClassic:     true,
		implicitOnCore:        true,
	})
}
