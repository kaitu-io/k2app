package wire

// JWTValidator validates a JWT token, device UDID, and optional client mode.
// Mode is "gateway" for router clients or "" for normal clients.
// Validators that don't support mode-based authorization should ignore the mode parameter.
//
// This is the single device-auth abstraction for both transports: the QUIC and
// TCP-WS metadata streams (StreamType 0x10) call it, and the enforce gate acts
// on its verdict. Implementations: NewUsersFileValidator, NewRemoteValidator,
// CachingValidator, ChainValidators.
type JWTValidator func(jwt, udid, mode string) bool
