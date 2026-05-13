package safenet

import (
	"net"
	"testing"
)

func TestValidateURLAcceptsPublicHTTPS(t *testing.T) {
	if err := ValidateURL("https://example.com/page"); err != nil {
		t.Fatalf("expected public URL to be allowed: %v", err)
	}
}

func TestValidateURLAcceptsPublicHTTP(t *testing.T) {
	if err := ValidateURL("http://example.com"); err != nil {
		t.Fatalf("expected public HTTP URL to be allowed: %v", err)
	}
}

func TestValidateURLBlocksFileScheme(t *testing.T) {
	err := ValidateURL("file:///etc/passwd")
	if err == nil {
		t.Fatal("expected file:// to be blocked")
	}
}

func TestValidateURLBlocksFTP(t *testing.T) {
	err := ValidateURL("ftp://example.com/file")
	if err == nil {
		t.Fatal("expected ftp:// to be blocked")
	}
}

func TestValidateURLBlocksLocalhost(t *testing.T) {
	err := ValidateURL("http://localhost:8080/secret")
	if err == nil {
		t.Fatal("expected localhost to be blocked")
	}
}

func TestValidateURLBlocksLoopbackIP(t *testing.T) {
	err := ValidateURL("http://127.0.0.1/admin")
	if err == nil {
		t.Fatal("expected 127.0.0.1 to be blocked")
	}
}

func TestValidateURLBlocksPrivateIP10(t *testing.T) {
	err := ValidateURL("http://10.0.0.1/internal")
	if err == nil {
		t.Fatal("expected 10.x to be blocked")
	}
}

func TestValidateURLBlocksPrivateIP172(t *testing.T) {
	err := ValidateURL("http://172.16.0.1/internal")
	if err == nil {
		t.Fatal("expected 172.16.x to be blocked")
	}
}

func TestValidateURLBlocksPrivateIP192(t *testing.T) {
	err := ValidateURL("http://192.168.1.1/router")
	if err == nil {
		t.Fatal("expected 192.168.x to be blocked")
	}
}

func TestValidateURLBlocksCloudMetadata(t *testing.T) {
	err := ValidateURL("http://169.254.169.254/latest/meta-data/")
	if err == nil {
		t.Fatal("expected cloud metadata IP to be blocked")
	}
}

func TestValidateURLBlocksEmptyHost(t *testing.T) {
	err := ValidateURL("http:///path")
	if err == nil {
		t.Fatal("expected empty host to be blocked")
	}
}

func TestValidateIPRejectsUnspecified(t *testing.T) {
	err := validateIP(net.IPv4zero)
	if err == nil {
		t.Fatal("expected 0.0.0.0 to be blocked")
	}
}

func TestIsBlockedHost(t *testing.T) {
	tests := []struct {
		host     string
		blocked  bool
	}{
		{"localhost", true},
		{"LOCALHOST", true},
		{"localhost.localdomain", true},
		{"example.com", false},
		{"", false},
	}
	for _, tt := range tests {
		got := IsBlockedHost(tt.host)
		if got != tt.blocked {
			t.Errorf("IsBlockedHost(%q) = %v, want %v", tt.host, got, tt.blocked)
		}
	}
}
