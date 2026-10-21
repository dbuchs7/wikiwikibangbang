package dest

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// The examples of AWS's documentation of signature version 4.
func TestSignatureVersion4(t *testing.T) {
	// "GET Object" of the S3 documentation.
	req, _ := http.NewRequest("GET", "https://examplebucket.s3.amazonaws.com/test.txt", nil)
	req.Header.Set("Range", "bytes=0-9")
	empty := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	req.Header.Set("x-amz-content-sha256", empty)
	at := time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)
	sign(req, "AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "us-east-1", "s3", at, empty,
		[]string{"host", "range", "x-amz-content-sha256", "x-amz-date"})
	if got := req.Header.Get("Authorization"); !strings.HasSuffix(got, "Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41") {
		t.Errorf("GET Object: %s", got)
	}
	// "get-vanilla" of the signature version 4 test suite.
	req, _ = http.NewRequest("GET", "https://example.amazonaws.com/", nil)
	sign(req, "AKIDEXAMPLE", "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY", "us-east-1", "service", time.Date(2015, 8, 30, 12, 36, 0, 0, time.UTC), empty,
		[]string{"host", "x-amz-date"})
	if got := req.Header.Get("Authorization"); got != "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20150830/us-east-1/service/aws4_request, SignedHeaders=host;x-amz-date, Signature=5fa00fa31553b73ebf1942676e86291e8372ff2a2260956d9b8aae1d763fbf31" {
		t.Errorf("get-vanilla: %s", got)
	}
	if got := canonicalQuery(map[string][]string{"prefix": {"a b/c"}, "list-type": {"2"}, "uploads": {""}}); got != "list-type=2&prefix=a%20b%2Fc&uploads=" {
		t.Errorf("query %s", got)
	}
}
