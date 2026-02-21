package config

// ValidationError represents a config validation failure for a specific field.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return "config: " + e.Field + ": " + e.Message
}

// ValidateClient validates a ClientConfig and returns a *ValidationError if invalid.
func ValidateClient(cfg *ClientConfig) error {
	return nil // RED: not implemented
}

// ValidateServer validates a ServerConfig and returns a *ValidationError if invalid.
func ValidateServer(cfg *ServerConfig) error {
	return nil // RED: not implemented
}
