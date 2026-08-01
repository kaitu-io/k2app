package wire

// AuthVerdict is the structured result of a device-auth validation.
//
// Code is FOUR-state, not three. The "no opinion" state exists because
// NewUsersFileValidator is keyed by udid and shared nodes deliberately run
// with an empty users file: if a users-file miss were an explicit rejection,
// it would shadow Center's real answer (including "unreachable") in every
// chain. Fail-closed applies only to Center saying no, never to Center being
// silent (spec §6.4).
//
//	OK     Code          meaning
//	true   0             authenticated
//	false  0             no opinion (this validator doesn't know the udid)
//	false  401/402/403   explicit rejection (only Center can issue these)
//	false  -1            cannot determine (network error / timeout / 5xx / decode failure)
//	false  -2            node fault: THIS node cannot perform auth at all
type AuthVerdict struct {
	OK     bool
	Code   int
	UserID uint64
	SvcExp int64 // Center's ServiceExpiredAt (unix seconds); 0 = no subscription clock (users_file path)
}

// Auth verdict codes that are NOT HTTP-derived. Both are negative so they can
// never collide with a Center-issued status code.
const (
	// AuthCodeUndetermined — Center was asked but did not answer (network
	// error, timeout, 5xx, decode failure). Transient and not this node's
	// fault: permissive serves, enforce rejects (spec §6.4).
	AuthCodeUndetermined = -1

	// AuthCodeNodeFault — THIS node cannot perform device auth at all: no
	// NodeSecret, unresolvable node IPv4, or a malformed request it built
	// itself. Center was never even asked.
	//
	// Why it is not folded into the fail-closed 401 tail (its behavior before
	// this code existed): a 401 is the wire's way of saying "your credentials
	// are invalid", and it reaches the user as an auth error. One node missing
	// K2_NODE_SECRET would then tell every device it serves that THEIR account
	// failed — sending the whole incident down the wrong diagnostic path while
	// the client, treating 401 as a client error, stops retrying instead of
	// racing to a healthy node. Nor can it be folded into -1: that would let a
	// permanently misconfigured node look like a transient Center blip forever.
	//
	// It is also deliberately NOT a passing verdict: a node that cannot
	// authenticate must never attribute traffic or count a connection as
	// authed — see resolveMetadataAuth, which maps this to a server-category
	// rejection so the rollout counters keep telling the truth.
	AuthCodeNodeFault = -2
)

// Validator validates a device credential (token, udid, optional mode) and
// returns a structured verdict. It replaces the old bool JWTValidator and is
// the single device-auth abstraction for BOTH transports: the QUIC and TCP-WS
// metadata streams (StreamType 0x10) call it, and the enforce gate acts on its
// verdict. Implementations: NewUsersFileValidator, NewRemoteValidator,
// CachingValidator, ChainValidators.
type Validator func(token, udid, mode string) AuthVerdict
