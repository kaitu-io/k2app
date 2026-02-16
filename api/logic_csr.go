package center

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wordgate/qtoolkit/log"
)

// CSRRequest represents a pending CSR request with challenge
type CSRRequest struct {
	ID               string
	PublicKey        crypto.PublicKey
	PublicKeyPEM     string
	Domains          []string
	VerificationPort int
	KeyType          string
	Challenge        *CSRChallenge
	CreatedAt        time.Time
}

// CSRChallenge represents the challenge data for verification
type CSRChallenge struct {
	Nonce     string    // Hex-encoded random nonce (32 bytes)
	Timestamp int64     // Challenge creation timestamp
	ExpiresAt int64     // Challenge expiry timestamp
	CreatedAt time.Time // Internal creation time
}

// CSRRequestStore stores pending CSR requests (in-memory with expiration)
type CSRRequestStore struct {
	mu       sync.RWMutex
	requests map[string]*CSRRequest
}

var (
	csrStore     *CSRRequestStore
	csrStoreOnce sync.Once

	// Challenge validity period (5 minutes)
	challengeValidityDuration = 5 * time.Minute

	// Certificate validity (1 year)
	certificateValidityDays = 365

	// Supported domain services (used for validation reference)
	_ = []string{"sslip.io", "nip.io"}

	// IPv4 domain pattern: {a}-{b}-{c}-{d}.sslip.io
	ipv4DomainPattern = regexp.MustCompile(`^(\d{1,3})-(\d{1,3})-(\d{1,3})-(\d{1,3})\.(sslip\.io|nip\.io)$`)

	// IPv6 domain pattern: uses dashes, -- for ::
	ipv6DomainPattern = regexp.MustCompile(`^([0-9a-fA-F-]+)\.(sslip\.io|nip\.io)$`)
)

// getCSRStore returns the singleton CSR request store
func getCSRStore() *CSRRequestStore {
	csrStoreOnce.Do(func() {
		csrStore = &CSRRequestStore{
			requests: make(map[string]*CSRRequest),
		}
		// Start cleanup goroutine
		go csrStore.cleanup()
	})
	return csrStore
}

// Store stores a CSR request
func (s *CSRRequestStore) Store(req *CSRRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests[req.ID] = req
}

// Get retrieves a CSR request by ID
func (s *CSRRequestStore) Get(id string) (*CSRRequest, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	req, ok := s.requests[id]
	return req, ok
}

// Delete removes a CSR request
func (s *CSRRequestStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.requests, id)
}

// cleanup periodically removes expired requests
func (s *CSRRequestStore) cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for id, req := range s.requests {
			if req.Challenge != nil && now.Unix() > req.Challenge.ExpiresAt {
				delete(s.requests, id)
			}
		}
		s.mu.Unlock()
	}
}

// ParsePublicKey parses a base64-encoded public key
func ParsePublicKey(publicKeyB64 string) (crypto.PublicKey, string, error) {
	pubKeyBytes, err := base64.StdEncoding.DecodeString(publicKeyB64)
	if err != nil {
		return nil, "", fmt.Errorf("failed to decode public key: %w", err)
	}

	// Try parsing as PKIX public key (SubjectPublicKeyInfo format)
	pubKey, err := x509.ParsePKIXPublicKey(pubKeyBytes)
	if err == nil {
		switch pk := pubKey.(type) {
		case *ecdsa.PublicKey:
			return pk, "ecdsa", nil
		case *rsa.PublicKey:
			return pk, "rsa", nil
		default:
			return nil, "", fmt.Errorf("unsupported public key type: %T", pubKey)
		}
	}

	// Try parsing as raw EC public key
	ecKey, err := x509.ParsePKCS1PublicKey(pubKeyBytes)
	if err == nil {
		return ecKey, "rsa", nil
	}

	return nil, "", fmt.Errorf("failed to parse public key: unsupported format")
}

// ValidateSSLIPDomain validates and extracts IP from sslip.io/nip.io domain
func ValidateSSLIPDomain(domain string) (net.IP, error) {
	domain = strings.ToLower(domain)

	// Check IPv4 pattern
	if matches := ipv4DomainPattern.FindStringSubmatch(domain); matches != nil {
		ipStr := fmt.Sprintf("%s.%s.%s.%s", matches[1], matches[2], matches[3], matches[4])
		ip := net.ParseIP(ipStr)
		if ip == nil {
			return nil, fmt.Errorf("invalid IPv4 in domain: %s", domain)
		}
		return ip.To4(), nil
	}

	// Check IPv6 pattern
	if matches := ipv6DomainPattern.FindStringSubmatch(domain); matches != nil {
		// Convert dashes back to colons, -- to ::
		ipStr := matches[1]
		ipStr = strings.ReplaceAll(ipStr, "--", "::")
		ipStr = strings.ReplaceAll(ipStr, "-", ":")
		ip := net.ParseIP(ipStr)
		if ip == nil {
			return nil, fmt.Errorf("invalid IPv6 in domain: %s", domain)
		}
		return ip, nil
	}

	return nil, fmt.Errorf("domain must use sslip.io or nip.io format: %s", domain)
}

// GenerateChallenge generates a new challenge for CSR verification
func GenerateChallenge() (*CSRChallenge, error) {
	// Generate 32-byte random nonce
	nonceBytes := make([]byte, 32)
	if _, err := rand.Read(nonceBytes); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	now := time.Now()
	return &CSRChallenge{
		Nonce:     hex.EncodeToString(nonceBytes),
		Timestamp: now.Unix(),
		ExpiresAt: now.Add(challengeValidityDuration).Unix(),
		CreatedAt: now,
	}, nil
}

// VerifySignature verifies a signature against the challenge nonce
func VerifySignature(pubKey crypto.PublicKey, nonce string, signatureB64 string) error {
	// Decode nonce
	nonceBytes, err := hex.DecodeString(nonce)
	if err != nil {
		return fmt.Errorf("failed to decode nonce: %w", err)
	}

	// Decode signature
	sigBytes, err := base64.StdEncoding.DecodeString(signatureB64)
	if err != nil {
		return fmt.Errorf("failed to decode signature: %w", err)
	}

	// Hash the nonce (SHA-256)
	hash := sha256.Sum256(nonceBytes)

	switch pk := pubKey.(type) {
	case *ecdsa.PublicKey:
		// ECDSA verification
		if !ecdsa.VerifyASN1(pk, hash[:], sigBytes) {
			return errors.New("ECDSA signature verification failed")
		}
		return nil

	case *rsa.PublicKey:
		// RSA-PKCS1v15 verification
		if err := rsa.VerifyPKCS1v15(pk, crypto.SHA256, hash[:], sigBytes); err != nil {
			return fmt.Errorf("RSA signature verification failed: %w", err)
		}
		return nil

	default:
		return fmt.Errorf("unsupported public key type: %T", pubKey)
	}
}

// VerifyDomainOwnership verifies that the domain resolves to the expected IP
// and that the slave is listening on the verification port
func VerifyDomainOwnership(ctx context.Context, domain string, port int, expectedIP net.IP, challenge *CSRChallenge, pubKey crypto.PublicKey) error {
	log.Infof(ctx, "[CSR] Verifying domain ownership: domain=%s, port=%d, expectedIP=%s", domain, port, expectedIP)

	// 1. DNS resolution check
	ips, err := net.LookupIP(domain)
	if err != nil {
		return fmt.Errorf("DNS lookup failed for %s: %w", domain, err)
	}

	found := false
	for _, ip := range ips {
		if ip.Equal(expectedIP) {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("domain %s does not resolve to %s (resolved: %v)", domain, expectedIP, ips)
	}

	// 2. TCP connection check with challenge-response
	// Use net.JoinHostPort to properly handle both IPv4 and IPv6 addresses
	addr := net.JoinHostPort(domain, fmt.Sprintf("%d", port))
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", addr, err)
	}
	defer conn.Close()

	// Set deadline for the verification exchange
	conn.SetDeadline(time.Now().Add(30 * time.Second))

	// Send challenge
	challengeData := fmt.Sprintf("KAITU-CSR-CHALLENGE:%s:%d\n", challenge.Nonce, challenge.Timestamp)
	if _, err := conn.Write([]byte(challengeData)); err != nil {
		return fmt.Errorf("failed to send challenge: %w", err)
	}

	// Read response (expect: "KAITU-CSR-RESPONSE:{base64-signature}\n")
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		return fmt.Errorf("failed to read challenge response: %w", err)
	}

	response := strings.TrimSpace(string(buf[:n]))
	if !strings.HasPrefix(response, "KAITU-CSR-RESPONSE:") {
		return fmt.Errorf("invalid challenge response format")
	}

	signatureB64 := strings.TrimPrefix(response, "KAITU-CSR-RESPONSE:")
	if err := VerifySignature(pubKey, challenge.Nonce, signatureB64); err != nil {
		return fmt.Errorf("challenge-response verification failed: %w", err)
	}

	log.Infof(ctx, "[CSR] Domain ownership verified: domain=%s", domain)
	return nil
}

// SignCSRCertificate signs a certificate for the given public key and domains
func SignCSRCertificate(ctx context.Context, pubKey crypto.PublicKey, domains []string) (certPEM, chainPEM []byte, serialNum string, expiresAt int64, err error) {
	log.Infof(ctx, "[CSR] Signing certificate for domains: %v", domains)

	// Get CA certificate and key
	caCertPEM, caKeyPEM, err := GetCa(ctx)
	if err != nil {
		return nil, nil, "", 0, fmt.Errorf("failed to get CA: %w", err)
	}

	// Parse CA certificate
	caCertBlock, _ := pem.Decode(caCertPEM)
	if caCertBlock == nil {
		return nil, nil, "", 0, errors.New("failed to decode CA certificate PEM")
	}
	caCert, err := x509.ParseCertificate(caCertBlock.Bytes)
	if err != nil {
		return nil, nil, "", 0, fmt.Errorf("failed to parse CA certificate: %w", err)
	}

	// Parse CA private key
	caKeyBlock, _ := pem.Decode(caKeyPEM)
	if caKeyBlock == nil {
		return nil, nil, "", 0, errors.New("failed to decode CA key PEM")
	}
	caKey, err := x509.ParseECPrivateKey(caKeyBlock.Bytes)
	if err != nil {
		return nil, nil, "", 0, fmt.Errorf("failed to parse CA private key: %w", err)
	}

	// Generate serial number
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, "", 0, fmt.Errorf("failed to generate serial number: %w", err)
	}

	// Create certificate template
	now := time.Now()
	notAfter := now.AddDate(0, 0, certificateValidityDays)

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName: domains[0], // First domain as CN
		},
		NotBefore:             now.Add(-10 * time.Minute),
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		DNSNames:              domains,
	}

	// Sign certificate
	certDER, err := x509.CreateCertificate(rand.Reader, template, caCert, pubKey, caKey)
	if err != nil {
		return nil, nil, "", 0, fmt.Errorf("failed to create certificate: %w", err)
	}

	// Encode certificate to PEM
	certPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	// Create certificate chain (cert + CA cert)
	chainPEM = append(certPEM, caCertPEM...)

	serialNum = hex.EncodeToString(serialNumber.Bytes())
	expiresAt = notAfter.Unix()

	log.Infof(ctx, "[CSR] Certificate signed successfully: serial=%s, expires=%s", serialNum, notAfter.Format(time.RFC3339))
	return certPEM, chainPEM, serialNum, expiresAt, nil
}

// CSRSubmitRequest represents the CSR submission request
type CSRSubmitRequest struct {
	PublicKey        string   `json:"publicKey" binding:"required"`
	Domains          []string `json:"domains" binding:"required,min=1"`
	VerificationPort int      `json:"verificationPort" binding:"required,min=1,max=65535"`
}

// CSRSubmitResponse represents the CSR submission response
type CSRSubmitResponse struct {
	RequestID             string                    `json:"requestId"`
	Challenge             *CSRChallengeResponse     `json:"challenge"`
	VerificationEndpoints []VerificationEndpointDTO `json:"verificationEndpoints"`
}

// CSRChallengeResponse is the challenge data in response
type CSRChallengeResponse struct {
	Nonce     string `json:"nonce"`
	Timestamp int64  `json:"timestamp"`
	ExpiresAt int64  `json:"expiresAt"`
}

// VerificationEndpointDTO is a verification endpoint in response
type VerificationEndpointDTO struct {
	Domain     string `json:"domain"`
	Port       int    `json:"port"`
	ExpectedIP string `json:"expectedIP"`
}

// CSRVerifyRequest represents the CSR verification request
type CSRVerifyRequest struct {
	RequestID       string `json:"requestId" binding:"required"`
	SignedChallenge string `json:"signedChallenge" binding:"required"`
}

// CSRVerifyResponse represents the CSR verification response
type CSRVerifyResponse struct {
	Certificate      string   `json:"certificate"`
	CertificateChain string   `json:"certificateChain"`
	IssuedAt         int64    `json:"issuedAt"`
	ExpiresAt        int64    `json:"expiresAt"`
	SerialNumber     string   `json:"serialNumber"`
	Domains          []string `json:"domains"`
}

// ProcessCSRSubmit processes a CSR submission request
// This is a public API - domain ownership is verified via challenge-response
func ProcessCSRSubmit(ctx context.Context, req *CSRSubmitRequest) (*CSRSubmitResponse, error) {
	log.Infof(ctx, "[CSR] Processing CSR submit: domains=%v, port=%d", req.Domains, req.VerificationPort)

	// 1. Parse and validate public key
	pubKey, keyType, err := ParsePublicKey(req.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("invalid public key: %w", err)
	}

	// 2. Validate domains and extract expected IPs
	verificationEndpoints := make([]VerificationEndpointDTO, 0, len(req.Domains))
	for _, domain := range req.Domains {
		domainIP, err := ValidateSSLIPDomain(domain)
		if err != nil {
			return nil, err
		}

		verificationEndpoints = append(verificationEndpoints, VerificationEndpointDTO{
			Domain:     domain,
			Port:       req.VerificationPort,
			ExpectedIP: domainIP.String(),
		})
	}

	// 3. Generate challenge
	challenge, err := GenerateChallenge()
	if err != nil {
		return nil, fmt.Errorf("failed to generate challenge: %w", err)
	}

	// 4. Store CSR request
	csrReq := &CSRRequest{
		ID:               uuid.New().String(),
		PublicKey:        pubKey,
		PublicKeyPEM:     req.PublicKey,
		Domains:          req.Domains,
		VerificationPort: req.VerificationPort,
		KeyType:          keyType,
		Challenge:        challenge,
		CreatedAt:        time.Now(),
	}
	getCSRStore().Store(csrReq)

	log.Infof(ctx, "[CSR] CSR request stored: requestId=%s, expiresAt=%d", csrReq.ID, challenge.ExpiresAt)

	return &CSRSubmitResponse{
		RequestID: csrReq.ID,
		Challenge: &CSRChallengeResponse{
			Nonce:     challenge.Nonce,
			Timestamp: challenge.Timestamp,
			ExpiresAt: challenge.ExpiresAt,
		},
		VerificationEndpoints: verificationEndpoints,
	}, nil
}

// ProcessCSRVerify processes a CSR verification request
// This is a public API - verification is done via challenge-response on the domain
func ProcessCSRVerify(ctx context.Context, req *CSRVerifyRequest) (*CSRVerifyResponse, error) {
	log.Infof(ctx, "[CSR] Processing CSR verify: requestId=%s", req.RequestID)

	// 1. Retrieve CSR request
	csrReq, found := getCSRStore().Get(req.RequestID)
	if !found {
		return nil, errors.New("CSR request not found or expired")
	}

	// 2. Check challenge expiry
	if time.Now().Unix() > csrReq.Challenge.ExpiresAt {
		getCSRStore().Delete(req.RequestID)
		return nil, errors.New("challenge has expired")
	}

	// 3. Verify signature
	if err := VerifySignature(csrReq.PublicKey, csrReq.Challenge.Nonce, req.SignedChallenge); err != nil {
		return nil, fmt.Errorf("signature verification failed: %w", err)
	}

	// 4. Verify domain ownership for each domain
	for _, domain := range csrReq.Domains {
		domainIP, err := ValidateSSLIPDomain(domain)
		if err != nil {
			return nil, err
		}

		if err := VerifyDomainOwnership(ctx, domain, csrReq.VerificationPort, domainIP, csrReq.Challenge, csrReq.PublicKey); err != nil {
			return nil, fmt.Errorf("domain verification failed for %s: %w", domain, err)
		}
	}

	// 5. Sign certificate
	certPEM, chainPEM, serialNum, expiresAt, err := SignCSRCertificate(ctx, csrReq.PublicKey, csrReq.Domains)
	if err != nil {
		return nil, fmt.Errorf("failed to sign certificate: %w", err)
	}

	// 6. Clean up CSR request
	getCSRStore().Delete(req.RequestID)

	log.Infof(ctx, "[CSR] Certificate issued successfully: requestId=%s, serial=%s", req.RequestID, serialNum)

	return &CSRVerifyResponse{
		Certificate:      string(certPEM),
		CertificateChain: string(chainPEM),
		IssuedAt:         time.Now().Unix(),
		ExpiresAt:        expiresAt,
		SerialNumber:     serialNum,
		Domains:          csrReq.Domains,
	}, nil
}
