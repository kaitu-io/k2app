// K2RouteShim.h — minimal vendored subset of <net/route.h> for iOS.
//
// The iOS SDK (device and simulator) does not ship net/route.h — the routing
// socket / sysctl PF_ROUTE interface is public ABI but its header is
// macOS-only. Apps that walk the routing table (defaultGatewayIPv4 in
// K2Plugin.swift) must vendor these definitions; this is the standard
// practice (WireGuard-iOS et al.).
//
// Copied verbatim from xnu bsd/net/route.h. This layout is kernel ABI:
// sysctl(NET_RT_FLAGS) writes rt_msghdr records in exactly this shape, and
// changing it would break every compiled binary — so the copy cannot rot.
//
// CocoaPods includes this header in the K2Plugin umbrella (podspec
// source_files covers *.h), which makes the struct and constants visible to
// the pod's Swift code with C-guaranteed layout. Do NOT redeclare these in
// Swift: Swift structs have no layout guarantee.

#ifndef K2RouteShim_h
#define K2RouteShim_h

#include <sys/types.h>

// These metrics are ignored by defaultGatewayIPv4 but are part of the
// rt_msghdr wire layout, so the struct must be complete.
struct rt_metrics {
	u_int32_t rmx_locks;      /* Kernel leaves these values alone */
	u_int32_t rmx_mtu;        /* MTU for this path */
	u_int32_t rmx_hopcount;   /* max hops expected */
	int32_t   rmx_expire;     /* lifetime for route, e.g. redirect */
	u_int32_t rmx_recvpipe;   /* inbound delay-bandwidth product */
	u_int32_t rmx_sendpipe;   /* outbound delay-bandwidth product */
	u_int32_t rmx_ssthresh;   /* outbound gateway buffer limit */
	u_int32_t rmx_rtt;        /* estimated round trip time */
	u_int32_t rmx_rttvar;     /* estimated rtt variance */
	u_int32_t rmx_pksent;     /* packets sent using this route */
	u_int32_t rmx_state;      /* route state */
	u_int32_t rmx_filler[3];  /* will be used for T/TCP later */
};

struct rt_msghdr {
	u_short   rtm_msglen;     /* to skip over non-understood messages */
	u_char    rtm_version;    /* future binary compatibility */
	u_char    rtm_type;       /* message type */
	u_short   rtm_index;      /* index for associated ifp */
	int       rtm_flags;      /* flags, incl. kern & message, e.g. DONE */
	int       rtm_addrs;      /* bitmask identifying sockaddrs in msg */
	pid_t     rtm_pid;        /* identify sender */
	int       rtm_seq;        /* for sender to identify action */
	int       rtm_errno;      /* why failed */
	int       rtm_use;        /* from rtentry */
	u_int32_t rtm_inits;      /* which metrics we are initializing */
	struct rt_metrics rtm_rmx; /* metrics themselves */
};

#define RTF_GATEWAY  0x2      /* destination is a gateway */

/* Bitmask values for rtm_addrs. */
#define RTA_DST      0x1      /* destination sockaddr present */
#define RTA_GATEWAY  0x2      /* gateway sockaddr present */

/* Index offsets for sockaddr array in reply. */
#define RTAX_DST     0        /* destination sockaddr present */
#define RTAX_GATEWAY 1        /* gateway sockaddr present */
#define RTAX_MAX     8        /* size of array to allocate */

#endif /* K2RouteShim_h */
