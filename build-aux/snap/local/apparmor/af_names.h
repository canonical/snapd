/*
  this file was generated on a Ubuntu mantic install from the upstream
  apparmor-4.0.0-alpha3 release tarball as follows:

  AA_VER=4.0.0-alpha3
  TARBALL_NAME="apparmor-${AA_VER/-/\~}"
  wget \
  "https://launchpad.net/apparmor/4.0/${AA_VER}/+download/${TARBALL_NAME}.tar.gz"
  tar xf "${TARBALL_NAME}.tar.gz"
  cd "${TARBALL_NAME}"
  make -C parser af_names.h

 */
#ifndef AF_UNSPEC
#  define AF_UNSPEC 0
#endif
AA_GEN_NET_ENT("unspec", AF_UNSPEC)

#ifndef AF_UNIX
#  define AF_UNIX 1
#endif
AA_GEN_NET_ENT("unix", AF_UNIX)

#ifndef AF_INET
#  define AF_INET 2
#endif
AA_GEN_NET_ENT("inet", AF_INET)

#ifndef AF_AX25
#  define AF_AX25 3
#endif
AA_GEN_NET_ENT("ax25", AF_AX25)

#ifndef AF_IPX
#  define AF_IPX 4
#endif
AA_GEN_NET_ENT("ipx", AF_IPX)

#ifndef AF_APPLETALK
#  define AF_APPLETALK 5
#endif
AA_GEN_NET_ENT("appletalk", AF_APPLETALK)

#ifndef AF_NETROM
#  define AF_NETROM 6
#endif
AA_GEN_NET_ENT("netrom", AF_NETROM)

#ifndef AF_BRIDGE
#  define AF_BRIDGE 7
#endif
AA_GEN_NET_ENT("bridge", AF_BRIDGE)

#ifndef AF_ATMPVC
#  define AF_ATMPVC 8
#endif
AA_GEN_NET_ENT("atmpvc", AF_ATMPVC)

#ifndef AF_X25
#  define AF_X25 9
#endif
AA_GEN_NET_ENT("x25", AF_X25)

#ifndef AF_INET6
#  define AF_INET6 10
#endif
AA_GEN_NET_ENT("inet6", AF_INET6)

#ifndef AF_ROSE
#  define AF_ROSE 11
#endif
AA_GEN_NET_ENT("rose", AF_ROSE)

#ifndef AF_NETBEUI
#  define AF_NETBEUI 13
#endif
AA_GEN_NET_ENT("netbeui", AF_NETBEUI)

#ifndef AF_SECURITY
#  define AF_SECURITY 14
#endif
AA_GEN_NET_ENT("security", AF_SECURITY)

#ifndef AF_KEY
#  define AF_KEY 15
#endif
AA_GEN_NET_ENT("key", AF_KEY)

#ifndef AF_NETLINK
#  define AF_NETLINK 16
#endif
AA_GEN_NET_ENT("netlink", AF_NETLINK)

#ifndef AF_PACKET
#  define AF_PACKET 17
#endif
AA_GEN_NET_ENT("packet", AF_PACKET)

#ifndef AF_ASH
#  define AF_ASH 18
#endif
AA_GEN_NET_ENT("ash", AF_ASH)

#ifndef AF_ECONET
#  define AF_ECONET 19
#endif
AA_GEN_NET_ENT("econet", AF_ECONET)

#ifndef AF_ATMSVC
#  define AF_ATMSVC 20
#endif
AA_GEN_NET_ENT("atmsvc", AF_ATMSVC)

#ifndef AF_RDS
#  define AF_RDS 21
#endif
AA_GEN_NET_ENT("rds", AF_RDS)

#ifndef AF_SNA
#  define AF_SNA 22
#endif
AA_GEN_NET_ENT("sna", AF_SNA)

#ifndef AF_IRDA
#  define AF_IRDA 23
#endif
AA_GEN_NET_ENT("irda", AF_IRDA)

#ifndef AF_PPPOX
#  define AF_PPPOX 24
#endif
AA_GEN_NET_ENT("pppox", AF_PPPOX)

#ifndef AF_WANPIPE
#  define AF_WANPIPE 25
#endif
AA_GEN_NET_ENT("wanpipe", AF_WANPIPE)

#ifndef AF_LLC
#  define AF_LLC 26
#endif
AA_GEN_NET_ENT("llc", AF_LLC)

#ifndef AF_IB
#  define AF_IB 27
#endif
AA_GEN_NET_ENT("ib", AF_IB)

#ifndef AF_MPLS
#  define AF_MPLS 28
#endif
AA_GEN_NET_ENT("mpls", AF_MPLS)

#ifndef AF_CAN
#  define AF_CAN 29
#endif
AA_GEN_NET_ENT("can", AF_CAN)

#ifndef AF_TIPC
#  define AF_TIPC 30
#endif
AA_GEN_NET_ENT("tipc", AF_TIPC)

#ifndef AF_BLUETOOTH
#  define AF_BLUETOOTH 31
#endif
AA_GEN_NET_ENT("bluetooth", AF_BLUETOOTH)

#ifndef AF_IUCV
#  define AF_IUCV 32
#endif
AA_GEN_NET_ENT("iucv", AF_IUCV)

#ifndef AF_RXRPC
#  define AF_RXRPC 33
#endif
AA_GEN_NET_ENT("rxrpc", AF_RXRPC)

#ifndef AF_ISDN
#  define AF_ISDN 34
#endif
AA_GEN_NET_ENT("isdn", AF_ISDN)

#ifndef AF_PHONET
#  define AF_PHONET 35
#endif
AA_GEN_NET_ENT("phonet", AF_PHONET)

#ifndef AF_IEEE802154
#  define AF_IEEE802154 36
#endif
AA_GEN_NET_ENT("ieee802154", AF_IEEE802154)

#ifndef AF_CAIF
#  define AF_CAIF 37
#endif
AA_GEN_NET_ENT("caif", AF_CAIF)

#ifndef AF_ALG
#  define AF_ALG 38
#endif
AA_GEN_NET_ENT("alg", AF_ALG)

#ifndef AF_NFC
#  define AF_NFC 39
#endif
AA_GEN_NET_ENT("nfc", AF_NFC)

#ifndef AF_VSOCK
#  define AF_VSOCK 40
#endif
AA_GEN_NET_ENT("vsock", AF_VSOCK)

#ifndef AF_KCM
#  define AF_KCM 41
#endif
AA_GEN_NET_ENT("kcm", AF_KCM)

#ifndef AF_QIPCRTR
#  define AF_QIPCRTR 42
#endif
AA_GEN_NET_ENT("qipcrtr", AF_QIPCRTR)

#ifndef AF_SMC
#  define AF_SMC 43
#endif
AA_GEN_NET_ENT("smc", AF_SMC)

#ifndef AF_XDP
#  define AF_XDP 44
#endif
AA_GEN_NET_ENT("xdp", AF_XDP)

#ifndef AF_MCTP
#  define AF_MCTP 45
#endif
AA_GEN_NET_ENT("mctp", AF_MCTP)


#define AA_AF_MAX 46

