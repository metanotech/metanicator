package storage

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	smithymiddleware "github.com/aws/smithy-go/middleware"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestS3StorageCustomEndpointDoesNotSignAcceptEncoding(t *testing.T) {
	type observedRequest struct {
		authorization      string
		acceptEncoding     string
		contentLength      int64
		contentSHA256      string
		contentEncoding    string
		decodedLength      string
		trailer            string
		transferEncoding   []string
		checksumHeaders    http.Header
		contentType        string
		contentDisposition string
		cacheControl       string
		body               []byte
	}

	for _, tc := range []struct {
		name      string
		payload   []byte
		streaming bool
	}{
		{name: "buffered upload", payload: []byte("buffered payload")},
		{name: "multi-megabyte streaming upload", payload: bytes.Repeat([]byte("streamed-payload-"), 256*1024), streaming: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			observed := make(chan observedRequest, 1)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("read request body: %v", err)
				}
				checksumHeaders := make(http.Header)
				for name, values := range r.Header {
					if strings.HasPrefix(strings.ToLower(name), "x-amz-checksum-") {
						checksumHeaders[name] = append([]string(nil), values...)
					}
				}
				observed <- observedRequest{
					authorization:      r.Header.Get("Authorization"),
					acceptEncoding:     r.Header.Get("Accept-Encoding"),
					contentLength:      r.ContentLength,
					contentSHA256:      r.Header.Get("X-Amz-Content-Sha256"),
					contentEncoding:    r.Header.Get("Content-Encoding"),
					decodedLength:      r.Header.Get("X-Amz-Decoded-Content-Length"),
					trailer:            r.Header.Get("X-Amz-Trailer"),
					transferEncoding:   append([]string(nil), r.TransferEncoding...),
					checksumHeaders:    checksumHeaders,
					contentType:        r.Header.Get("Content-Type"),
					contentDisposition: r.Header.Get("Content-Disposition"),
					cacheControl:       r.Header.Get("Cache-Control"),
					body:               body,
				}
				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(server.Close)

			// This transport wrapper represents Cloudflare changing the header
			// after SigV4 signing but before RustFS receives the request.
			baseTransport := server.Client().Transport
			proxyClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				proxied := request.Clone(request.Context())
				proxied.Header.Set("Accept-Encoding", "gzip, br")
				return baseTransport.RoundTrip(proxied)
			})}
			client := s3.New(s3.Options{
				Region:                     "us-east-1",
				Credentials:                aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider("AKID", "SECRET", "")),
				UsePathStyle:               true,
				HTTPClient:                 proxyClient,
				RequestChecksumCalculation: aws.RequestChecksumCalculationWhenSupported,
			}, func(options *s3.Options) {
				configureS3Endpoint(options, server.URL)
			})
			store := &S3Storage{
				client:       client,
				bucket:       "test-bucket",
				region:       "us-east-1",
				endpointURL:  server.URL,
				usePathStyle: true,
			}

			const contentType = "application/octet-stream"
			const filename = "media.bin"
			var err error
			if tc.streaming {
				_, err = store.UploadStream(
					context.Background(), "uploads/media.bin",
					io.LimitReader(bytes.NewReader(tc.payload), int64(len(tc.payload))),
					int64(len(tc.payload)), contentType, filename,
				)
			} else {
				_, err = store.Upload(
					context.Background(), "uploads/media.bin", tc.payload,
					contentType, filename,
				)
			}
			if err != nil {
				t.Fatalf("upload: %v", err)
			}

			got := <-observed
			if got.acceptEncoding != "gzip, br" {
				t.Fatalf("accept-encoding at origin = %q, want proxy-mutated value", got.acceptEncoding)
			}
			signedHeaders := signedHeadersFromAuthorization(got.authorization)
			if len(signedHeaders) == 0 {
				t.Fatalf("authorization header has no SignedHeaders: %q", got.authorization)
			}
			for _, header := range signedHeaders {
				if header == "accept-encoding" {
					t.Fatalf("accept-encoding unexpectedly signed: %q", got.authorization)
				}
			}
			if got.contentLength != int64(len(tc.payload)) || len(got.transferEncoding) != 0 {
				t.Fatalf("request is not fixed length: content_length=%d transfer=%v", got.contentLength, got.transferEncoding)
			}
			if got.contentSHA256 != "UNSIGNED-PAYLOAD" {
				t.Fatalf("x-amz-content-sha256 = %q, want UNSIGNED-PAYLOAD", got.contentSHA256)
			}
			if got.contentEncoding != "" || got.decodedLength != "" || got.trailer != "" || len(got.checksumHeaders) != 0 {
				t.Fatalf("unexpected chunking or checksum headers: content_encoding=%q decoded_length=%q trailer=%q checksums=%v",
					got.contentEncoding, got.decodedLength, got.trailer, got.checksumHeaders)
			}
			if !bytes.Equal(got.body, tc.payload) {
				t.Fatalf("request body changed: got %d bytes, want %d", len(got.body), len(tc.payload))
			}
			if got.contentType != contentType || got.contentDisposition != ContentDisposition(contentType, filename) || got.cacheControl != "max-age=432000,public" {
				t.Fatalf("upload metadata changed: content_type=%q content_disposition=%q cache_control=%q",
					got.contentType, got.contentDisposition, got.cacheControl)
			}
		})
	}
}

func signedHeadersFromAuthorization(authorization string) []string {
	for _, part := range strings.Split(authorization, ",") {
		part = strings.TrimSpace(part)
		if value, ok := strings.CutPrefix(part, "SignedHeaders="); ok {
			return strings.Split(value, ";")
		}
	}
	return nil
}

func TestConfigureS3EndpointAcceptEncodingMiddleware(t *testing.T) {
	t.Run("custom endpoint middleware is idempotent", func(t *testing.T) {
		options := s3.Options{}
		configureS3Endpoint(&options, "https://s3.metanotech.com")
		if len(options.APIOptions) != 1 {
			t.Fatalf("APIOptions count = %d, want 1", len(options.APIOptions))
		}

		stack := smithymiddleware.NewStack("test", func() interface{} { return nil })
		if err := options.APIOptions[0](stack); err != nil {
			t.Fatalf("apply middleware to stack without target: %v", err)
		}
		if err := options.APIOptions[0](stack); err != nil {
			t.Fatalf("apply middleware twice: %v", err)
		}
	})

	t.Run("standard AWS endpoint is unchanged", func(t *testing.T) {
		options := s3.Options{}
		configureS3Endpoint(&options, "https://s3.us-east-1.amazonaws.com")
		if len(options.APIOptions) != 0 {
			t.Fatalf("APIOptions count = %d, want 0", len(options.APIOptions))
		}
	})
}

func TestS3StorageUploadStreamUsesFixedLengthRequest(t *testing.T) {
	type observedRequest struct {
		contentLength int64
		decodedLength string
		encoding      string
		transfer      []string
		body          string
	}
	observed := make(chan observedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		observed <- observedRequest{
			contentLength: r.ContentLength,
			decodedLength: r.Header.Get("X-Amz-Decoded-Content-Length"),
			encoding:      r.Header.Get("Content-Encoding"),
			transfer:      append([]string(nil), r.TransferEncoding...),
			body:          string(body),
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := s3.New(s3.Options{
		Region:       "us-east-1",
		Credentials:  aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider("AKID", "SECRET", "")),
		BaseEndpoint: aws.String(server.URL),
		UsePathStyle: true,
	})
	store := &S3Storage{
		client:       client,
		bucket:       "test-bucket",
		region:       "us-east-1",
		endpointURL:  server.URL,
		usePathStyle: true,
	}
	payload := "streamed payload"
	nonSeekable := io.LimitReader(strings.NewReader(payload), int64(len(payload)))
	if _, err := store.UploadStream(context.Background(), "uploads/media.bin", nonSeekable, int64(len(payload)), "application/octet-stream", "media.bin"); err != nil {
		t.Fatalf("UploadStream: %v", err)
	}

	got := <-observed
	if got.contentLength != int64(len(payload)) || len(got.transfer) != 0 {
		t.Fatalf("request is not a fixed-length upload: content_length=%d decoded_length=%q encoding=%q transfer=%v",
			got.contentLength, got.decodedLength, got.encoding, got.transfer)
	}
	if got.body != payload {
		t.Fatalf("request body = %q, want %q", got.body, payload)
	}
}

func TestS3StorageUploadStreamRejectsUnknownLength(t *testing.T) {
	store := &S3Storage{}
	if _, err := store.UploadStream(context.Background(), "uploads/media.bin", strings.NewReader("payload"), 0, "application/octet-stream", "media.bin"); err == nil {
		t.Fatal("UploadStream accepted an unknown content length")
	}
}

type observedChecksumRequest struct {
	contentSHA256 string
	trailer       string
	checksumCRC32 string
	encoding      string
	body          string
}

// newChecksumRecordingServer records the checksum-relevant wire details of a
// single upload. TLS matters: the SDK only switches to a trailing checksum when
// the request is HTTPS (see the IsHTTPS gate in the checksum middleware), and
// production talks to OSS/S3 over HTTPS. A plain HTTP server would take the
// header-checksum branch instead and never reproduce the trailer.
func newChecksumRecordingServer(t *testing.T, observed chan<- observedChecksumRequest) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		observed <- observedChecksumRequest{
			contentSHA256: r.Header.Get("X-Amz-Content-Sha256"),
			trailer:       r.Header.Get("X-Amz-Trailer"),
			checksumCRC32: r.Header.Get("X-Amz-Checksum-Crc32"),
			encoding:      r.Header.Get("Content-Encoding"),
			body:          string(body),
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	return server
}

// newChecksumTestStore points a store at the recording server. endpointURL is
// the field Upload branches on, so pass "" to exercise the default AWS shape
// and server.URL to exercise an S3-compatible backend.
func newChecksumTestStore(server *httptest.Server, endpointURL string) *S3Storage {
	return &S3Storage{
		client: s3.New(s3.Options{
			Region:       "us-east-1",
			Credentials:  aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider("AKID", "SECRET", "")),
			BaseEndpoint: aws.String(server.URL),
			UsePathStyle: true,
			HTTPClient:   server.Client(),
			// NewS3StorageFromEnv builds its client through
			// config.LoadDefaultConfig, which resolves this to
			// WhenSupported. s3.New leaves it unset, and an unset value
			// emits no checksum at all — so without pinning the production
			// default here the test passes even with the fix removed.
			RequestChecksumCalculation: aws.RequestChecksumCalculationWhenSupported,
		}),
		bucket:       "test-bucket",
		region:       "us-east-1",
		endpointURL:  endpointURL,
		usePathStyle: true,
	}
}

func TestS3StorageUploadPathsAvoidChecksumTrailer(t *testing.T) {
	newStore := func(server *httptest.Server) *S3Storage {
		return newChecksumTestStore(server, server.URL)
	}

	// S3-compatible backends such as Aliyun OSS and Tencent COS reject
	// aws-chunked framing outright, so neither upload path may emit a
	// trailing checksum.
	assertNoTrailer := func(t *testing.T, got observedChecksumRequest, payload string) {
		t.Helper()
		if got.contentSHA256 == "STREAMING-UNSIGNED-PAYLOAD-TRAILER" {
			t.Fatalf("x-amz-content-sha256 = %q, want a non-chunked value", got.contentSHA256)
		}
		if got.trailer != "" {
			t.Fatalf("x-amz-trailer = %q, want empty", got.trailer)
		}
		if got.checksumCRC32 != "" {
			t.Fatalf("x-amz-checksum-crc32 = %q, want empty", got.checksumCRC32)
		}
		if strings.Contains(got.encoding, "aws-chunked") {
			t.Fatalf("content-encoding = %q, want no aws-chunked framing", got.encoding)
		}
		if got.body != payload {
			t.Fatalf("request body = %q, want %q", got.body, payload)
		}
	}

	t.Run("buffered upload", func(t *testing.T) {
		observed := make(chan observedChecksumRequest, 1)
		server := newChecksumRecordingServer(t, observed)
		payload := "buffered payload"

		if _, err := newStore(server).Upload(
			context.Background(), "uploads/media.bin", []byte(payload),
			"application/octet-stream", "media.bin",
		); err != nil {
			t.Fatalf("Upload: %v", err)
		}
		assertNoTrailer(t, <-observed, payload)
	})

	t.Run("streaming upload", func(t *testing.T) {
		observed := make(chan observedChecksumRequest, 1)
		server := newChecksumRecordingServer(t, observed)
		payload := "streamed payload"
		nonSeekable := io.LimitReader(strings.NewReader(payload), int64(len(payload)))

		if _, err := newStore(server).UploadStream(
			context.Background(), "uploads/media.bin", nonSeekable, int64(len(payload)),
			"application/octet-stream", "media.bin",
		); err != nil {
			t.Fatalf("UploadStream: %v", err)
		}
		assertNoTrailer(t, <-observed, payload)
	})
}

// usesAWSEndpoint decides whether a request keeps the SDK's checksum trailer,
// so a wrong answer either reintroduces the OSS/COS failure or strips the
// checksum from an AWS bucket that requires one. Both directions have to hold
// on the host label boundary.
func TestS3StorageUsesAWSEndpoint(t *testing.T) {
	for _, tc := range []struct {
		endpointURL string
		want        bool
	}{
		{"", true}, // unset: the SDK resolves the default AWS endpoint
		{"https://s3.us-east-1.amazonaws.com", true},
		{"https://S3.US-EAST-1.AMAZONAWS.COM", true},
		{"https://bucket.s3.us-east-1.amazonaws.com:443", true},
		{"https://bucket.s3.amazonaws.com.", true},
		{"https://bucket.vpce-0abc-xyz.s3.us-east-1.vpce.amazonaws.com", true},
		{"https://s3-fips.us-gov-west-1.amazonaws.com", true},
		{"https://s3.dualstack.us-east-1.amazonaws.com", true},
		{"https://s3.cn-north-1.amazonaws.com.cn", true},
		{"s3.us-east-1.amazonaws.com", true}, // scheme-less but still AWS

		{"https://oss-cn-shanghai-internal.aliyuncs.com", false},
		{"https://cos.ap-shanghai.myqcloud.com", false},
		{"https://notamazonaws.com", false},
		{"https://s3.amazonaws.com.example.net", false},
		{"https://rustfs.internal:9000?probe=amazonaws.com", false},
		{"https://rustfs.internal:9000/amazonaws.com", false},
		{"oss-cn-shanghai-internal.aliyuncs.com", false},
	} {
		t.Run(tc.endpointURL, func(t *testing.T) {
			s := &S3Storage{endpointURL: tc.endpointURL}
			if got := s.usesAWSEndpoint(); got != tc.want {
				t.Fatalf("usesAWSEndpoint(%q) = %v, want %v", tc.endpointURL, got, tc.want)
			}
		})
	}
}

// The OSS/COS workaround costs the request its client-side checksum, so it
// must not reach AWS: real AWS S3 accepts the aws-chunked trailer, and buckets
// carrying a default Object Lock retention require a checksum to be present.
// The store below is shaped the way NewS3StorageFromEnv leaves it when
// AWS_ENDPOINT_URL is unset; the client is pointed at a local TLS server only
// so the request can be observed.
func TestS3StorageAWSUploadKeepsChecksumTrailer(t *testing.T) {
	for _, endpointURL := range []string{
		"",
		"https://s3.us-east-1.amazonaws.com",
	} {
		t.Run("endpoint="+endpointURL, func(t *testing.T) {
			observed := make(chan observedChecksumRequest, 1)
			server := newChecksumRecordingServer(t, observed)
			payload := "buffered payload"

			if _, err := newChecksumTestStore(server, endpointURL).Upload(
				context.Background(), "uploads/media.bin", []byte(payload),
				"application/octet-stream", "media.bin",
			); err != nil {
				t.Fatalf("Upload: %v", err)
			}

			got := <-observed
			if got.contentSHA256 != "STREAMING-UNSIGNED-PAYLOAD-TRAILER" {
				t.Fatalf("x-amz-content-sha256 = %q, want the SDK default trailer flow", got.contentSHA256)
			}
			if !strings.HasPrefix(got.trailer, "x-amz-checksum-") {
				t.Fatalf("x-amz-trailer = %q, want a checksum trailer", got.trailer)
			}
		})
	}
}

func TestS3StorageKeyFromURL_CustomEndpointPreservesNestedKey(t *testing.T) {
	s := &S3Storage{
		bucket:      "test-bucket",
		endpointURL: "http://localhost:9000",
	}

	rawURL := "http://localhost:9000/test-bucket/uploads/abc/file.png"

	if got := s.KeyFromURL(rawURL); got != "uploads/abc/file.png" {
		t.Fatalf("KeyFromURL(%q) = %q, want %q", rawURL, got, "uploads/abc/file.png")
	}
}

func TestS3StoragePresignGet(t *testing.T) {
	store := &S3Storage{
		client: s3.New(s3.Options{
			Region:      "us-east-1",
			Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider("AKID", "SECRET", "")),
		}),
		bucket: "test-bucket",
	}

	got, err := store.PresignGet(context.Background(), "uploads/abc/file.txt", 5*time.Minute)
	if err != nil {
		t.Fatalf("PresignGet: %v", err)
	}
	for _, want := range []string{
		"https://test-bucket.s3.us-east-1.amazonaws.com/uploads/abc/file.txt",
		"X-Amz-Signature=",
		"X-Amz-Expires=300",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("presigned URL %q does not contain %q", got, want)
		}
	}
}

func TestS3StoragePresignGetWithContentDisposition(t *testing.T) {
	store := &S3Storage{
		client: s3.New(s3.Options{
			Region:      "us-east-1",
			Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider("AKID", "SECRET", "")),
		}),
		bucket: "test-bucket",
	}

	got, err := store.PresignGetWithContentDisposition(
		context.Background(),
		"uploads/abc/file.txt",
		5*time.Minute,
		`attachment; filename="report.txt"`,
	)
	if err != nil {
		t.Fatalf("PresignGetWithContentDisposition: %v", err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse presigned URL: %v", err)
	}
	if got := u.Query().Get("response-content-disposition"); got != `attachment; filename="report.txt"` {
		t.Fatalf("response-content-disposition = %q", got)
	}
	if sig := u.Query().Get("X-Amz-Signature"); sig == "" {
		t.Fatalf("missing X-Amz-Signature in %q", got)
	}
}

func TestS3StorageKeyFromURL_CustomEndpointWithTrailingSlash(t *testing.T) {
	s := &S3Storage{
		bucket:      "test-bucket",
		endpointURL: "http://localhost:9000/",
	}

	rawURL := "http://localhost:9000/test-bucket/uploads/abc/file.png"

	if got := s.KeyFromURL(rawURL); got != "uploads/abc/file.png" {
		t.Fatalf("KeyFromURL(%q) = %q, want %q", rawURL, got, "uploads/abc/file.png")
	}
}

func TestS3StorageKeyFromURL_CustomEndpointVirtualHostedStylePreservesNestedKey(t *testing.T) {
	s := &S3Storage{
		bucket:       "test-bucket",
		endpointURL:  "https://objects.example.com",
		usePathStyle: false,
	}

	rawURL := "https://test-bucket.objects.example.com/uploads/abc/file.png"

	if got := s.KeyFromURL(rawURL); got != "uploads/abc/file.png" {
		t.Fatalf("KeyFromURL(%q) = %q, want %q", rawURL, got, "uploads/abc/file.png")
	}
}

func TestS3StorageKeyFromURL_VirtualHostedStylePreservesNestedKey(t *testing.T) {
	s := &S3Storage{
		bucket: "test-bucket",
		region: "us-east-1",
	}

	rawURL := "https://test-bucket.s3.us-east-1.amazonaws.com/uploads/abc/file.png"

	if got := s.KeyFromURL(rawURL); got != "uploads/abc/file.png" {
		t.Fatalf("KeyFromURL(%q) = %q, want %q", rawURL, got, "uploads/abc/file.png")
	}
}

func TestS3StorageKeyFromURL_PathStylePreservesNestedKey(t *testing.T) {
	s := &S3Storage{
		bucket: "bucket.with.dots",
		region: "us-east-1",
	}

	rawURL := "https://s3.us-east-1.amazonaws.com/bucket.with.dots/uploads/abc/file.png"

	if got := s.KeyFromURL(rawURL); got != "uploads/abc/file.png" {
		t.Fatalf("KeyFromURL(%q) = %q, want %q", rawURL, got, "uploads/abc/file.png")
	}
}

func TestS3StorageKeyFromURL_LegacyBucketOnlyHostStillRoundTrips(t *testing.T) {
	// Old records written before the suffix bug was fixed look like
	// "https://<bucket>/<key>". They were broken at fetch time but were still
	// stored, so KeyFromURL must continue to recognise that prefix when we
	// migrate or delete those records.
	s := &S3Storage{
		bucket: "test-bucket",
		region: "us-east-1",
	}

	rawURL := "https://test-bucket/uploads/abc/file.png"

	if got := s.KeyFromURL(rawURL); got != "uploads/abc/file.png" {
		t.Fatalf("KeyFromURL(%q) = %q, want %q", rawURL, got, "uploads/abc/file.png")
	}
}

func TestS3StorageKeyFromURL_PreviousProviderEndpointPreservesNestedKey(t *testing.T) {
	store := &S3Storage{
		bucket:       "test-bucket",
		endpointURL:  "http://rustfs:9000",
		usePathStyle: true,
	}

	cases := []string{
		"http://minio:9000/test-bucket/uploads/abc/file.png",
		"https://test-bucket.minio.example.com/uploads/abc/file.png",
	}
	for _, rawURL := range cases {
		if got := store.KeyFromURL(rawURL); got != "uploads/abc/file.png" {
			t.Fatalf("KeyFromURL(%q) = %q, want %q", rawURL, got, "uploads/abc/file.png")
		}
	}
}

func TestLooksLikeS3Hostname(t *testing.T) {
	cases := []struct {
		bucket string
		want   bool
	}{
		{"my-bucket", false},
		{"bucket.with.dots", false},
		{"my-bucket.s3.us-east-1.amazonaws.com", true},
		{"my-bucket.s3.amazonaws.com", true},
		{"s3.us-east-1.amazonaws.com", true},
	}
	for _, tc := range cases {
		t.Run(tc.bucket, func(t *testing.T) {
			if got := looksLikeS3Hostname(tc.bucket); got != tc.want {
				t.Fatalf("looksLikeS3Hostname(%q) = %v, want %v", tc.bucket, got, tc.want)
			}
		})
	}
}

func TestS3UsePathStyleFromEnv(t *testing.T) {
	t.Run("defaults to false without custom endpoint", func(t *testing.T) {
		t.Setenv("S3_USE_PATH_STYLE", "")
		if got := s3UsePathStyleFromEnv(""); got {
			t.Fatalf("s3UsePathStyleFromEnv() = %v, want false", got)
		}
	})

	t.Run("defaults to true with custom endpoint", func(t *testing.T) {
		t.Setenv("S3_USE_PATH_STYLE", "")
		if got := s3UsePathStyleFromEnv("https://objects.example.com"); !got {
			t.Fatalf("s3UsePathStyleFromEnv() = %v, want true", got)
		}
	})

	t.Run("can disable path style for custom endpoint", func(t *testing.T) {
		t.Setenv("S3_USE_PATH_STYLE", "false")
		if got := s3UsePathStyleFromEnv("https://objects.example.com"); got {
			t.Fatalf("s3UsePathStyleFromEnv() = %v, want false", got)
		}
	})

	t.Run("invalid value keeps default", func(t *testing.T) {
		t.Setenv("S3_USE_PATH_STYLE", "maybe")
		if got := s3UsePathStyleFromEnv("https://objects.example.com"); !got {
			t.Fatalf("s3UsePathStyleFromEnv() = %v, want true", got)
		}
	})
}

func TestNewS3StorageFromEnv_ConfiguresEndpointPathStyle(t *testing.T) {
	t.Run("defaults custom endpoints to path style", func(t *testing.T) {
		t.Setenv("S3_BUCKET", "test-bucket")
		t.Setenv("S3_REGION", "us-east-1")
		t.Setenv("AWS_ACCESS_KEY_ID", "AKID")
		t.Setenv("AWS_SECRET_ACCESS_KEY", "SECRET")
		t.Setenv("AWS_ENDPOINT_URL", "https://objects.example.com")
		t.Setenv("S3_USE_PATH_STYLE", "")

		store := NewS3StorageFromEnv()
		if store == nil {
			t.Fatal("NewS3StorageFromEnv() = nil")
		}
		if !store.usePathStyle {
			t.Fatalf("usePathStyle = false, want true")
		}
		if !store.client.Options().UsePathStyle {
			t.Fatalf("client UsePathStyle = false, want true")
		}
	})

	t.Run("can disable path style for custom endpoints", func(t *testing.T) {
		t.Setenv("S3_BUCKET", "test-bucket")
		t.Setenv("S3_REGION", "us-east-1")
		t.Setenv("AWS_ACCESS_KEY_ID", "AKID")
		t.Setenv("AWS_SECRET_ACCESS_KEY", "SECRET")
		t.Setenv("AWS_ENDPOINT_URL", "https://objects.example.com")
		t.Setenv("S3_USE_PATH_STYLE", "false")

		store := NewS3StorageFromEnv()
		if store == nil {
			t.Fatal("NewS3StorageFromEnv() = nil")
		}
		if store.usePathStyle {
			t.Fatalf("usePathStyle = true, want false")
		}
		if store.client.Options().UsePathStyle {
			t.Fatalf("client UsePathStyle = true, want false")
		}
		if got, want := store.uploadedURL("uploads/file.txt"), "https://test-bucket.objects.example.com/uploads/file.txt"; got != want {
			t.Fatalf("uploadedURL() = %q, want %q", got, want)
		}
	})

	t.Run("supports RustFS S3 environment variables", func(t *testing.T) {
		t.Setenv("S3_BUCKET", "rustfs-bucket")
		t.Setenv("S3_REGION", "")
		t.Setenv("S3_ENDPOINT_URL", "http://rustfs:9000")
		t.Setenv("S3_ACCESS_KEY", "rustfsadmin")
		t.Setenv("S3_SECRET_KEY", "rustfssecret")
		t.Setenv("AWS_ENDPOINT_URL", "https://legacy.example.com")
		t.Setenv("AWS_ACCESS_KEY_ID", "LEGACY")
		t.Setenv("AWS_SECRET_ACCESS_KEY", "LEGACY_SECRET")

		store := NewS3StorageFromEnv()
		if store == nil {
			t.Fatal("NewS3StorageFromEnv() = nil for RustFS variables")
		}
		if store.bucket != "rustfs-bucket" {
			t.Fatalf("bucket = %q, want %q", store.bucket, "rustfs-bucket")
		}
		if store.endpointURL != "http://rustfs:9000" {
			t.Fatalf("endpointURL = %q, want %q", store.endpointURL, "http://rustfs:9000")
		}
		if store.region != "us-east-1" {
			t.Fatalf("region = %q, want %q", store.region, "us-east-1")
		}
		if !store.usePathStyle {
			t.Fatalf("usePathStyle = false, want true")
		}
		creds, err := store.client.Options().Credentials.Retrieve(context.Background())
		if err != nil {
			t.Fatalf("retrieve credentials: %v", err)
		}
		if creds.AccessKeyID != "rustfsadmin" || creds.SecretAccessKey != "rustfssecret" {
			t.Fatalf("credentials = %q/%q, want RustFS credentials", creds.AccessKeyID, creds.SecretAccessKey)
		}
	})
}

func TestS3StorageUploadedURL(t *testing.T) {
	const key = "uploads/abc/file.png"

	cases := []struct {
		name         string
		bucket       string
		region       string
		cdnDomain    string
		endpointURL  string
		usePathStyle bool
		want         string
	}{
		{
			name:   "default aws virtual hosted style",
			bucket: "test-bucket",
			region: "us-east-1",
			want:   "https://test-bucket.s3.us-east-1.amazonaws.com/uploads/abc/file.png",
		},
		{
			name:   "default aws path style when bucket contains dots",
			bucket: "bucket.with.dots",
			region: "us-east-1",
			want:   "https://s3.us-east-1.amazonaws.com/bucket.with.dots/uploads/abc/file.png",
		},
		{
			name:      "cdn only",
			bucket:    "test-bucket",
			region:    "us-east-1",
			cdnDomain: "cdn.example.com",
			want:      "https://cdn.example.com/uploads/abc/file.png",
		},
		{
			name:         "endpoint path style",
			bucket:       "test-bucket",
			region:       "us-east-1",
			endpointURL:  "http://localhost:9000",
			usePathStyle: true,
			want:         "http://localhost:9000/test-bucket/uploads/abc/file.png",
		},
		{
			name:         "endpoint path style with trailing slash",
			bucket:       "test-bucket",
			region:       "us-east-1",
			endpointURL:  "http://localhost:9000/",
			usePathStyle: true,
			want:         "http://localhost:9000/test-bucket/uploads/abc/file.png",
		},
		{
			name:         "endpoint virtual hosted style",
			bucket:       "test-bucket",
			region:       "us-east-1",
			endpointURL:  "https://objects.example.com",
			usePathStyle: false,
			want:         "https://test-bucket.objects.example.com/uploads/abc/file.png",
		},
		{
			name:         "endpoint and cdn both set prefers cdn",
			bucket:       "test-bucket",
			region:       "us-east-1",
			cdnDomain:    "cdn.example.com",
			endpointURL:  "http://localhost:9000",
			usePathStyle: false,
			want:         "https://cdn.example.com/uploads/abc/file.png",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &S3Storage{
				bucket:       tc.bucket,
				region:       tc.region,
				cdnDomain:    tc.cdnDomain,
				endpointURL:  tc.endpointURL,
				usePathStyle: tc.usePathStyle,
			}
			if got := s.uploadedURL(key); got != tc.want {
				t.Fatalf("uploadedURL() = %q, want %q", got, tc.want)
			}
		})
	}
}
