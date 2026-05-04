package validation

import (
	"strings"
	"testing"
)

func TestValidateB2Config(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *B2Config
		wantErr string
	}{
		{
			name: "valid config",
			cfg: &B2Config{
				Account:    "abc123",
				Key:        "k0123456789abcdef",
				Bucket:     "my-bucket",
				RemoteName: "b2",
				RemotePath: "/photos",
			},
			wantErr: "",
		},
		{
			name: "missing account",
			cfg: &B2Config{
				Key:    "k0123456789abcdef",
				Bucket: "my-bucket",
			},
			wantErr: "B2 account ID is required",
		},
		{
			name: "missing key",
			cfg: &B2Config{
				Account: "abc123",
				Bucket:  "my-bucket",
			},
			wantErr: "B2 application key is required",
		},
		{
			name: "missing bucket",
			cfg: &B2Config{
				Account: "abc123",
				Key:     "k0123456789abcdef",
			},
			wantErr: "B2 bucket name is required",
		},
		{
			name: "invalid endpoint",
			cfg: &B2Config{
				Account:  "abc123",
				Key:      "k0123456789abcdef",
				Bucket:   "my-bucket",
				Endpoint: "ftp://evil.com",
			},
			wantErr: "B2 endpoint must be a valid HTTP(S) URL",
		},
		{
			name: "path traversal in remote path",
			cfg: &B2Config{
				Account:    "abc123",
				Key:        "k0123456789abcdef",
				Bucket:     "my-bucket",
				RemotePath: "/photos/../../etc",
			},
			wantErr: "remote path contains path traversal",
		},
		{
			name: "defaults applied",
			cfg: &B2Config{
				Account: "abc123",
				Key:     "k0123456789abcdef",
				Bucket:  "my-bucket",
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateB2Config(tt.cfg)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

func TestValidateB2BucketName(t *testing.T) {
	tests := []struct {
		name    string
		bucket  string
		wantErr string
	}{
		{"valid simple", "my-bucket", ""},
		{"valid digits", "bucket123", ""},
		{"valid single char repeated", "aaa", ""},
		{"valid max length", strings.Repeat("a", 63), ""},
		{"too short", "ab", "at least 3 characters"},
		{"too long", strings.Repeat("a", 64), "at most 63 characters"},
		{"starts with hyphen", "-mybucket", "must start and end"},
		{"ends with hyphen", "mybucket-", "must start and end"},
		{"uppercase", "MyBucket", "must start and end"},
		{"consecutive hyphens", "my--bucket", "consecutive hyphens"},
		{"contains underscore", "my_bucket", "must start and end"},
		{"contains dot", "my.bucket", "must start and end"},
		{"whitespace only", "   ", "must start and end"},
		{"contains null byte", "abc\x00def", "invalid characters"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateB2BucketName(tt.bucket)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

func TestB2RegionsNotEmpty(t *testing.T) {
	if len(B2Regions) == 0 {
		t.Fatal("B2Regions should not be empty")
	}
	for _, r := range B2Regions {
		if r.ID == "" || r.Name == "" || r.Endpoint == "" {
			t.Errorf("B2Region %+v has empty fields", r)
		}
		if !strings.HasPrefix(r.Endpoint, "https://") {
			t.Errorf("B2Region %q endpoint should use HTTPS: %s", r.ID, r.Endpoint)
		}
	}
}

func TestBuildB2RcloneConfig(t *testing.T) {
	cfg := &B2Config{
		Account:    "myaccount",
		Key:        "mykey",
		Bucket:     "mybucket",
		RemoteName: "b2backup",
	}

	data, err := BuildB2RcloneConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := ValidateRcloneConfig(data)
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if !result.Valid {
		t.Fatalf("generated config invalid: %v", result.Errors)
	}

	configStr := string(data)
	if !strings.Contains(configStr, "[b2backup]") {
		t.Errorf("config missing section [b2backup]")
	}
	if !strings.Contains(configStr, "type = b2") {
		t.Errorf("config missing type = b2")
	}
	if !strings.Contains(configStr, "account = myaccount") {
		t.Errorf("config missing account")
	}
	if !strings.Contains(configStr, "key = mykey") {
		t.Errorf("config missing key")
	}
}

func TestBuildB2RcloneConfigWithEndpoint(t *testing.T) {
	cfg := &B2Config{
		Account:    "acc",
		Key:        "key",
		Bucket:     "bucket",
		Endpoint:   "https://s3.us-west-001.backblazeb2.com",
	}

	data, err := BuildB2RcloneConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	configStr := string(data)
	if !strings.Contains(configStr, "endpoint = https://s3.us-west-001.backblazeb2.com") {
		t.Errorf("config missing endpoint: %s", configStr)
	}
}
