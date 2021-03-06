package s3

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/atlassian/git-lob/Godeps/_workspace/src/github.com/mitchellh/goamz/aws"
)

const (
	ISO8601BasicFormat      = "20060102T150405Z"
	ISO8601BasicFormatShort = "20060102"
)

/*
The V4Signer encapsulates all of the functionality to sign a request with the AWS
Signature Version 4 Signing Process. (http://goo.gl/u1OWZz)
*/
type V4Signer struct {
	auth        aws.Auth
	serviceName string
	region      aws.Region
	// Add the x-amz-content-sha256 header
	IncludeXAmzContentSha256 bool
}

/*
Return a new instance of a V4Signer capable of signing AWS requests.
*/
func NewV4Signer(auth aws.Auth, serviceName string, region aws.Region) *V4Signer {
	return &V4Signer{
		auth:        auth,
		serviceName: serviceName,
		region:      region,
		IncludeXAmzContentSha256: false,
	}
}

/*
Sign a request according to the AWS Signature Version 4 Signing Process. (http://goo.gl/u1OWZz)

The signed request will include an "x-amz-date" header with a current timestamp if a valid "x-amz-date"
or "date" header was not available in the original request. In addition, AWS Signature Version 4 requires
the "host" header to be a signed header, therefor the Sign method will manually set a "host" header from
the request.Host.

The signed request will include a new "Authorization" header indicating that the request has been signed.

Any changes to the request after signing the request will invalidate the signature.
*/
func (s *V4Signer) Sign(req *http.Request) {
	req.Header.Set("host", req.Host) // host header must be included as a signed header
	payloadHash := s.payloadHash(req)
	if s.IncludeXAmzContentSha256 {
		req.Header.Set("x-amz-content-sha256", payloadHash) // x-amz-content-sha256 contains the payload hash
	}
	t := s.requestTime(req)                           // Get request time
	creq := s.canonicalRequest(req, payloadHash)      // Build canonical request
	sts := s.stringToSign(t, creq)                    // Build string to sign
	signature := s.signature(t, sts)                  // Calculate the AWS Signature Version 4
	auth := s.authorization(req.Header, t, signature) // Create Authorization header value
	req.Header.Set("Authorization", auth)             // Add Authorization header to request
	return
}

/*
requestTime method will parse the time from the request "x-amz-date" or "date" headers.
If the "x-amz-date" header is present, that will take priority over the "date" header.
If neither header is defined or we are unable to parse either header as a valid date
then we will create a new "x-amz-date" header with the current time.
*/
func (s *V4Signer) requestTime(req *http.Request) time.Time {

	// Get "x-amz-date" header
	date := req.Header.Get("x-amz-date")

	// Attempt to parse as ISO8601BasicFormat
	t, err := time.Parse(ISO8601BasicFormat, date)
	if err == nil {
		return t
	}

	// Attempt to parse as http.TimeFormat
	t, err = time.Parse(http.TimeFormat, date)
	if err == nil {
		req.Header.Set("x-amz-date", t.Format(ISO8601BasicFormat))
		return t
	}

	// Get "date" header
	date = req.Header.Get("date")

	// Attempt to parse as http.TimeFormat
	t, err = time.Parse(http.TimeFormat, date)
	if err == nil {
		return t
	}

	// Create a current time header to be used
	t = time.Now().UTC()
	req.Header.Set("x-amz-date", t.Format(ISO8601BasicFormat))
	return t
}

/*
canonicalRequest method creates the canonical request according to Task 1 of the AWS Signature Version 4 Signing Process. (http://goo.gl/eUUZ3S)

    CanonicalRequest =
      HTTPRequestMethod + '\n' +
      CanonicalURI + '\n' +
      CanonicalQueryString + '\n' +
      CanonicalHeaders + '\n' +
      SignedHeaders + '\n' +
      HexEncode(Hash(Payload))

payloadHash is optional; use the empty string and it will be calculated from the request
*/
func (s *V4Signer) canonicalRequest(req *http.Request, payloadHash string) string {
	if payloadHash == "" {
		payloadHash = s.payloadHash(req)
	}
	c := new(bytes.Buffer)
	fmt.Fprintf(c, "%s\n", req.Method)
	fmt.Fprintf(c, "%s\n", s.canonicalURI(req.URL))
	fmt.Fprintf(c, "%s\n", s.canonicalQueryString(req.URL))
	fmt.Fprintf(c, "%s\n\n", s.canonicalHeaders(req.Header))
	fmt.Fprintf(c, "%s\n", s.signedHeaders(req.Header))
	fmt.Fprintf(c, "%s", payloadHash)
	return c.String()
}

func (s *V4Signer) canonicalURI(u *url.URL) string {
	canonicalPath := u.RequestURI()
	if u.RawQuery != "" {
		canonicalPath = canonicalPath[:len(canonicalPath)-len(u.RawQuery)-1]
	}
	slash := strings.HasSuffix(canonicalPath, "/")
	canonicalPath = path.Clean(canonicalPath)
	if canonicalPath != "/" && slash {
		canonicalPath += "/"
	}
	return canonicalPath
}

func (s *V4Signer) canonicalQueryString(u *url.URL) string {
	var a []string
	for k, vs := range u.Query() {
		k = url.QueryEscape(k)
		for _, v := range vs {
			if v == "" {
				a = append(a, k+"=")
			} else {
				v = url.QueryEscape(v)
				a = append(a, k+"="+v)
			}
		}
	}
	sort.Strings(a)
	return strings.Join(a, "&")
}

func (s *V4Signer) canonicalHeaders(h http.Header) string {
	i, a := 0, make([]string, len(h))
	for k, v := range h {
		for j, w := range v {
			v[j] = strings.Trim(w, " ")
		}
		sort.Strings(v)
		a[i] = strings.ToLower(k) + ":" + strings.Join(v, ",")
		i++
	}
	sort.Strings(a)
	return strings.Join(a, "\n")
}

func (s *V4Signer) signedHeaders(h http.Header) string {
	i, a := 0, make([]string, len(h))
	for k, _ := range h {
		a[i] = strings.ToLower(k)
		i++
	}
	sort.Strings(a)
	return strings.Join(a, ";")
}

func (s *V4Signer) payloadHash(req *http.Request) string {
	var b []byte
	if req.Body == nil {
		b = []byte("")
	} else {
		var err error
		b, err = ioutil.ReadAll(req.Body)
		if err != nil {
			// TODO: I REALLY DON'T LIKE THIS PANIC!!!!
			panic(err)
		}
	}
	req.Body = ioutil.NopCloser(bytes.NewBuffer(b))
	return s.hash(string(b))
}

/*
stringToSign method creates the string to sign accorting to Task 2 of the AWS Signature Version 4 Signing Process. (http://goo.gl/es1PAu)

    StringToSign  =
      Algorithm + '\n' +
      RequestDate + '\n' +
      CredentialScope + '\n' +
      HexEncode(Hash(CanonicalRequest))
*/
func (s *V4Signer) stringToSign(t time.Time, creq string) string {
	w := new(bytes.Buffer)
	fmt.Fprint(w, "AWS4-HMAC-SHA256\n")
	fmt.Fprintf(w, "%s\n", t.Format(ISO8601BasicFormat))
	fmt.Fprintf(w, "%s\n", s.credentialScope(t))
	fmt.Fprintf(w, "%s", s.hash(creq))
	return w.String()
}

func (s *V4Signer) credentialScope(t time.Time) string {
	return fmt.Sprintf("%s/%s/%s/aws4_request", t.Format(ISO8601BasicFormatShort), s.region.Name, s.serviceName)
}

/*
signature method calculates the AWS Signature Version 4 according to Task 3 of the AWS Signature Version 4 Signing Process. (http://goo.gl/j0Yqe1)

    signature = HexEncode(HMAC(derived-signing-key, string-to-sign))
*/
func (s *V4Signer) signature(t time.Time, sts string) string {
	h := s.hmac(s.derivedKey(t), []byte(sts))
	return fmt.Sprintf("%x", h)
}

/*
derivedKey method derives a signing key to be used for signing a request.

    kSecret = Your AWS Secret Access Key
    kDate = HMAC("AWS4" + kSecret, Date)
    kRegion = HMAC(kDate, Region)
    kService = HMAC(kRegion, Service)
    kSigning = HMAC(kService, "aws4_request")
*/
func (s *V4Signer) derivedKey(t time.Time) []byte {
	h := s.hmac([]byte("AWS4"+s.auth.SecretKey), []byte(t.Format(ISO8601BasicFormatShort)))
	h = s.hmac(h, []byte(s.region.Name))
	h = s.hmac(h, []byte(s.serviceName))
	h = s.hmac(h, []byte("aws4_request"))
	return h
}

/*
authorization method generates the authorization header value.
*/
func (s *V4Signer) authorization(header http.Header, t time.Time, signature string) string {
	w := new(bytes.Buffer)
	fmt.Fprint(w, "AWS4-HMAC-SHA256 ")
	fmt.Fprintf(w, "Credential=%s/%s, ", s.auth.AccessKey, s.credentialScope(t))
	fmt.Fprintf(w, "SignedHeaders=%s, ", s.signedHeaders(header))
	fmt.Fprintf(w, "Signature=%s", signature)
	return w.String()
}

// hash method calculates the sha256 hash for a given string
func (s *V4Signer) hash(in string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s", in)
	return fmt.Sprintf("%x", h.Sum(nil))
}

// hmac method calculates the sha256 hmac for a given slice of bytes
func (s *V4Signer) hmac(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}
