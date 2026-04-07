package megamcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "HTTPS passes",
			url:     "https://raw.githubusercontent.com/mvanhorn/printing-press-library/main",
			wantErr: false,
		},
		{
			name:    "HTTP rejected",
			url:     "http://example.com",
			wantErr: true,
			errMsg:  "must use HTTPS",
		},
		{
			name:    "empty scheme rejected",
			url:     "://example.com",
			wantErr: true,
			errMsg:  "invalid URL",
		},
		{
			name:    "loopback rejected",
			url:     "https://127.0.0.1/test",
			wantErr: true,
			errMsg:  "loopback",
		},
		{
			name:    "localhost hostname passes format check (DNS check at request time)",
			url:     "https://localhost/test",
			wantErr: false,
		},
		{
			name:    "private IP 10.x rejected",
			url:     "https://10.0.0.1/test",
			wantErr: true,
			errMsg:  "private",
		},
		{
			name:    "private IP 192.168.x rejected",
			url:     "https://192.168.1.1/test",
			wantErr: true,
			errMsg:  "private",
		},
		{
			name:    "no hostname rejected",
			url:     "https:///path",
			wantErr: true,
			errMsg:  "no hostname",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateBaseURL(tt.url)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSafeHTTPClient(t *testing.T) {
	// SafeHTTPClient should return a non-nil client with a custom transport.
	client := SafeHTTPClient(30 * time.Second)
	require.NotNil(t, client)
	assert.Equal(t, 30*time.Second, client.Timeout)
	assert.NotNil(t, client.Transport, "should have custom transport with safe dialer")
}

func TestSafeDialer_RejectsLoopback(t *testing.T) {
	d := &safeDialer{inner: &net.Dialer{Timeout: 5 * time.Second}}
	ctx := context.Background()

	// Connecting to 127.0.0.1 should be rejected.
	_, err := d.DialContext(ctx, "tcp", "127.0.0.1:443")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loopback")
}

func TestSanitizeText(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{
			name:   "normal text preserved",
			input:  "Hello, world!",
			maxLen: 100,
			want:   "Hello, world!",
		},
		{
			name:   "control chars stripped",
			input:  "Hello\x00World\x01Test\x1F",
			maxLen: 100,
			want:   "HelloWorldTest",
		},
		{
			name:   "newline preserved",
			input:  "line1\nline2",
			maxLen: 100,
			want:   "line1\nline2",
		},
		{
			name:   "tab preserved",
			input:  "col1\tcol2",
			maxLen: 100,
			want:   "col1\tcol2",
		},
		{
			name:   "length limited",
			input:  "abcdefghij",
			maxLen: 5,
			want:   "abcde",
		},
		{
			name:   "zero maxLen means no limit",
			input:  "abcdefghij",
			maxLen: 0,
			want:   "abcdefghij",
		},
		{
			name:   "empty string",
			input:  "",
			maxLen: 100,
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeText(tt.input, tt.maxLen)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestVerifyChecksum(t *testing.T) {
	data := []byte("hello world")
	h := sha256.Sum256(data)
	validChecksum := "sha256:" + hex.EncodeToString(h[:])

	tests := []struct {
		name     string
		data     []byte
		expected string
		wantErr  bool
		errMsg   string
	}{
		{
			name:     "matching hash passes",
			data:     data,
			expected: validChecksum,
			wantErr:  false,
		},
		{
			name:     "mismatched hash fails",
			data:     []byte("different data"),
			expected: validChecksum,
			wantErr:  true,
			errMsg:   "checksum mismatch",
		},
		{
			name:     "empty expected fails",
			data:     data,
			expected: "",
			wantErr:  true,
			errMsg:   "empty",
		},
		{
			name:     "wrong format fails",
			data:     data,
			expected: "md5:abc123",
			wantErr:  true,
			errMsg:   "unsupported checksum format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := VerifyChecksum(tt.data, tt.expected)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestComputeChecksum(t *testing.T) {
	data := []byte("hello world")
	checksum := ComputeChecksum(data)
	assert.True(t, len(checksum) > 7, "checksum should have sha256: prefix plus hex")
	assert.NoError(t, VerifyChecksum(data, checksum), "ComputeChecksum output should verify")
}

func TestValidateSlug(t *testing.T) {
	tests := []struct {
		name    string
		slug    string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid simple slug",
			slug:    "espn",
			wantErr: false,
		},
		{
			name:    "valid hyphenated slug",
			slug:    "steam-web",
			wantErr: false,
		},
		{
			name:    "path traversal rejected",
			slug:    "../etc",
			wantErr: true,
			errMsg:  "path traversal",
		},
		{
			name:    "double dot in middle rejected",
			slug:    "foo..bar",
			wantErr: true,
			errMsg:  "path traversal",
		},
		{
			name:    "forward slash rejected",
			slug:    "foo/bar",
			wantErr: true,
			errMsg:  "forward slash",
		},
		{
			name:    "backslash rejected",
			slug:    "foo\\bar",
			wantErr: true,
			errMsg:  "backslash",
		},
		{
			name:    "empty slug rejected",
			slug:    "",
			wantErr: true,
			errMsg:  "empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSlug(tt.slug)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateCachePath(t *testing.T) {
	root := t.TempDir()

	tests := []struct {
		name    string
		path    string
		root    string
		wantErr bool
	}{
		{
			name:    "path under root passes",
			path:    filepath.Join(root, "manifests", "espn", "tools-manifest.json"),
			root:    root,
			wantErr: false,
		},
		{
			name:    "escape attempt rejected",
			path:    filepath.Join(root, "..", "outside"),
			root:    root,
			wantErr: true,
		},
		{
			name:    "exact root passes",
			path:    root,
			root:    root,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCachePath(tt.path, tt.root)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "escapes cache root")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
