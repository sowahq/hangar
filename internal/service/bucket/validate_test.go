package bucket

import "testing"

func TestValidBucketName(t *testing.T) {
	tests := []struct {
		name   string
		bucket string
		valid  bool
	}{
		{"reserved admin", "admin", false},
		{"reserved status", "status", false},
		{"too short", "ab", false},
		{"too long", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", false},
		{"min length", "abc", true},
		{"max length 63", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", true},
		{"starts with dash", "-abc", false},
		{"starts with dot", ".abc", false},
		{"ends with dash", "abc-", false},
		{"ends with dot", "abc.", false},
		{"uppercase rejected", "Abc", false},
		{"underscore rejected", "ab_c", false},
		{"space rejected", "ab c", false},
		{"consecutive dots rejected", "ab..cd", false},
		{"dot after dash rejected", "ab-.cd", false},
		{"dash after dot rejected", "ab.-cd", false},
		{"valid simple", "my-bucket", true},
		{"valid with dot", "my.bucket", true},
		{"valid with digits", "bucket123", true},
		{"ipv4 rejected", "192.168.1.1", false},
		{"xn-- prefix rejected", "xn--abc", false},
		{"-s3alias suffix rejected", "mybucket-s3alias", false},
		{"--ol-s3 suffix rejected", "mybucket--ol-s3", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := validBucketName(tc.bucket)
			if got != tc.valid {
				t.Errorf("validBucketName(%q): got=%v want=%v", tc.bucket, got, tc.valid)
			}
		})
	}
}

func TestBucketNameErrorWrapping(t *testing.T) {
	if err := BucketName("admin"); err == nil {
		t.Error("BucketName(admin) returned nil, want error")
	}
	if err := BucketName("valid-bucket"); err != nil {
		t.Errorf("BucketName(valid-bucket): unexpected err: %v", err)
	}
}
