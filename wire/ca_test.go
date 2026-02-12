package wire

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"testing"
)

func TestBuildDefaultCAPool(t *testing.T) {
	pool := BuildDefaultCAPool()
	if pool == nil {
		t.Fatal("BuildDefaultCAPool returned nil")
	}

	// Verify the Kaitu CA PEM was parsed by checking it can be used to verify
	// a cert signed by it (or at minimum, that the pool is non-empty).
	// We parse the embedded PEM independently and check it's valid.
	ok := x509.NewCertPool().AppendCertsFromPEM(kaituCAPEM)
	if !ok {
		t.Fatal("kaituCAPEM is not a valid PEM certificate")
	}
}

func TestIsBlockedCA(t *testing.T) {
	tests := []struct {
		name    string
		subject []string
		issuer  []string
		want    bool
	}{
		{
			name:    "CNNIC in subject",
			subject: []string{"CNNIC"},
			want:    true,
		},
		{
			name:    "WoSign in subject",
			subject: []string{"WoSign CA Limited"},
			want:    true,
		},
		{
			name:    "WoTrus in issuer",
			issuer:  []string{"WoTrus CA"},
			want:    true,
		},
		{
			name:    "CFCA in subject",
			subject: []string{"CFCA"},
			want:    true,
		},
		{
			name:    "GDCA in subject",
			subject: []string{"GDCA TrustAUTH R5 ROOT"},
			want:    true,
		},
		{
			name:    "SHECA in issuer",
			issuer:  []string{"SHECA Root CA"},
			want:    true,
		},
		{
			name:    "China Financial Certification Authority in subject",
			subject: []string{"China Financial Certification Authority"},
			want:    true,
		},
		{
			name:    "Shanghai Electronic Certification Authority in issuer",
			issuer:  []string{"Shanghai Electronic Certification Authority Co., Ltd."},
			want:    true,
		},
		{
			name:    "Guang Dong Certificate Authority in subject",
			subject: []string{"Guang Dong Certificate Authority"},
			want:    true,
		},
		{
			name:    "case insensitive match",
			subject: []string{"cnnic SSL"},
			want:    true,
		},
		{
			name:    "normal CA not blocked",
			subject: []string{"Let's Encrypt"},
			issuer:  []string{"ISRG Root X1"},
			want:    false,
		},
		{
			name:    "DigiCert not blocked",
			subject: []string{"DigiCert Inc"},
			issuer:  []string{"DigiCert Global Root CA"},
			want:    false,
		},
		{
			name:    "empty orgs not blocked",
			subject: nil,
			issuer:  nil,
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cert := &x509.Certificate{
				Subject: pkix.Name{Organization: tt.subject},
				Issuer:  pkix.Name{Organization: tt.issuer},
			}
			got := IsBlockedCA(cert)
			if got != tt.want {
				t.Errorf("IsBlockedCA() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildBlockedCAVerifier(t *testing.T) {
	verifier := buildBlockedCAVerifier()

	t.Run("rejects blocked CA in chain", func(t *testing.T) {
		blockedCert := &x509.Certificate{
			Issuer: pkix.Name{Organization: []string{"CNNIC ROOT"}},
		}
		normalCert := &x509.Certificate{
			Subject: pkix.Name{Organization: []string{"example.com"}},
		}
		chains := [][]*x509.Certificate{{normalCert, blockedCert}}
		err := verifier(nil, chains)
		if err == nil {
			t.Error("verifier should reject chain containing blocked CA")
		}
	})

	t.Run("accepts normal chain", func(t *testing.T) {
		cert1 := &x509.Certificate{
			Subject: pkix.Name{Organization: []string{"example.com"}},
			Issuer:  pkix.Name{Organization: []string{"DigiCert"}},
		}
		cert2 := &x509.Certificate{
			Subject: pkix.Name{Organization: []string{"DigiCert"}},
			Issuer:  pkix.Name{Organization: []string{"DigiCert Root"}},
		}
		chains := [][]*x509.Certificate{{cert1, cert2}}
		err := verifier(nil, chains)
		if err != nil {
			t.Errorf("verifier should accept normal chain: %v", err)
		}
	})

	t.Run("accepts empty chains", func(t *testing.T) {
		err := verifier(nil, nil)
		if err != nil {
			t.Errorf("verifier should accept empty chains: %v", err)
		}
	})
}

func TestBuildClientTLSConfig_PinPriority(t *testing.T) {
	echRaw := []byte{0xfe, 0x0d, 0x00, 0x10}

	pair, err := GenerateCertPair()
	if err != nil {
		t.Fatalf("GenerateCertPair: %v", err)
	}

	cfg := Config{
		Host:        "real.server.com",
		Port:        443,
		ECH:         base64.StdEncoding.EncodeToString(echRaw),
		Fingerprint: "chrome",
		Pin:         pair.FormatPins(),
		Insecure:    true, // Pin should take precedence over insecure
	}

	tlsCfg, _, err := BuildClientTLSConfig(cfg)
	if err != nil {
		t.Fatalf("BuildClientTLSConfig failed: %v", err)
	}

	if !tlsCfg.InsecureSkipVerify {
		t.Error("InsecureSkipVerify should be true when Pin is set")
	}
	if tlsCfg.VerifyPeerCertificate == nil {
		t.Error("VerifyPeerCertificate should be set (pin verifier) when Pin is set")
	}
	// RootCAs should not be set when pin is used
	if tlsCfg.RootCAs != nil {
		t.Error("RootCAs should be nil when Pin is set")
	}
}

func TestBuildClientTLSConfig_Insecure(t *testing.T) {
	echRaw := []byte{0xfe, 0x0d, 0x00, 0x10}

	cfg := Config{
		Host:        "real.server.com",
		Port:        443,
		ECH:         base64.StdEncoding.EncodeToString(echRaw),
		Fingerprint: "chrome",
		Insecure:    true,
	}

	tlsCfg, _, err := BuildClientTLSConfig(cfg)
	if err != nil {
		t.Fatalf("BuildClientTLSConfig failed: %v", err)
	}

	if !tlsCfg.InsecureSkipVerify {
		t.Error("InsecureSkipVerify should be true when Insecure is set")
	}
	if tlsCfg.VerifyPeerCertificate != nil {
		t.Error("VerifyPeerCertificate should be nil when only Insecure is set (no pin)")
	}
	if tlsCfg.RootCAs != nil {
		t.Error("RootCAs should be nil when Insecure is set")
	}
}

func TestBuildClientTLSConfig_Default(t *testing.T) {
	echRaw := []byte{0xfe, 0x0d, 0x00, 0x10}

	cfg := Config{
		Host:        "real.server.com",
		Port:        443,
		ECH:         base64.StdEncoding.EncodeToString(echRaw),
		Fingerprint: "chrome",
		// No pin, no insecure → default CA pool path
	}

	tlsCfg, _, err := BuildClientTLSConfig(cfg)
	if err != nil {
		t.Fatalf("BuildClientTLSConfig failed: %v", err)
	}

	if tlsCfg.InsecureSkipVerify {
		t.Error("InsecureSkipVerify should be false in default mode")
	}
	if tlsCfg.RootCAs == nil {
		t.Error("RootCAs should be set (system + Kaitu CA) in default mode")
	}
	if tlsCfg.VerifyPeerCertificate == nil {
		t.Error("VerifyPeerCertificate should be set (blocked CA verifier) in default mode")
	}
}
