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
type AuthVerdict struct {
	OK     bool
	Code   int
	UserID uint64
	SvcExp int64 // Center's ServiceExpiredAt (unix seconds); 0 = no subscription clock (users_file path)
}

// Validator validates a device credential (token, udid, optional mode) and
// returns a structured verdict. It replaces the old bool JWTValidator and is
// the single device-auth abstraction for BOTH transports: the QUIC and TCP-WS
// metadata streams (StreamType 0x10) call it, and the enforce gate acts on its
// verdict. Implementations: NewUsersFileValidator, NewRemoteValidator,
// CachingValidator, ChainValidators.
type Validator func(token, udid, mode string) AuthVerdict
