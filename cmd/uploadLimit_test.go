package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	rearm "github.com/relizaio/rearm-client-go"
)

// The CLI's own words for an HTTP 413 (task PFA-10, PFA-3 design 3.3 and 4.3, tests T-9 to T-13).

const uploadLimitTail = " exceeds the upload limit of this server (HTTP 413). The default limit is 50 MB, but it may be different on this server; it is set by the server's administrator (Helm value max_upload_size_in_MB)."

const batchHint = " For a batch, send fewer releases per call."

// T-9: the message with the request size, found through a wrapped error.
func TestUploadLimitMessageNamesTheRequestSize(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", &rearm.StatusError{StatusCode: 413, RequestBytes: 94371840})
	msg, ok := uploadLimitMessage(err, false)
	if !ok {
		t.Fatal("a 413 is rewritten")
	}
	if want := "the request of 90.0 MB" + uploadLimitTail; msg != want {
		t.Fatalf("got  %q\nwant %q", msg, want)
	}
}

// T-10: the batch hint, appended after a space.
func TestUploadLimitMessageForABatchAddsTheHint(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", &rearm.StatusError{StatusCode: 413, RequestBytes: 94371840})
	msg, ok := uploadLimitMessage(err, true)
	if !ok {
		t.Fatal("a 413 is rewritten")
	}
	if want := "the request of 90.0 MB" + uploadLimitTail + batchHint; msg != want {
		t.Fatalf("got  %q\nwant %q", msg, want)
	}
}

// T-11: only a 413 is rewritten, and no size is invented when the client did not know it.
func TestUploadLimitMessageOnlyForA413(t *testing.T) {
	cases := []struct {
		name string
		err  error
		ok   bool
		want string
	}{
		{"413 with no body sent", &rearm.StatusError{StatusCode: 413, RequestBytes: 0}, true, "the request" + uploadLimitTail},
		{"413 of unknown size", &rearm.StatusError{StatusCode: 413, RequestBytes: -1}, true, "the request" + uploadLimitTail},
		{"401", &rearm.StatusError{StatusCode: 401, RequestBytes: 94371840}, false, ""},
		{"500", &rearm.StatusError{StatusCode: 500, RequestBytes: 94371840}, false, ""},
		{"GraphQL errors", rearm.GraphQLErrors{{Message: "x"}}, false, ""},
		{"plain error", errors.New("rearm: request failed with status 413"), false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			msg, ok := uploadLimitMessage(c.err, false)
			if ok != c.ok || msg != c.want {
				t.Fatalf("got (%q, %v), want (%q, %v)", msg, ok, c.want, c.ok)
			}
		})
	}
}

// T-12: every command that prints through describeError gets the message; other statuses keep their text.
func TestDescribeErrorRendersA413AsTheUploadLimitMessage(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", &rearm.StatusError{StatusCode: 413, RequestBytes: 94371840})
	if got, want := describeError(err), "the request of 90.0 MB"+uploadLimitTail; got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	if got, want := describeError(&rearm.StatusError{StatusCode: 401}), "rearm: request failed with status 401"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// T-13: a real 413 answer with an HTML page, through the key client the CLI builds and the session
// report upload path, reaches the words with the size the server saw.
func TestA413AnswerReachesTheUploadLimitMessageWithTheSizeSent(t *testing.T) {
	withSessionGlobals(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("REARM_NO_TOKEN_CACHE", "true")
	savedClient := apiClient
	t.Cleanup(func() { apiClient = savedClient })

	var (
		mu       sync.Mutex
		uploads  int
		seenSize int64
	)
	page := "<html>\r\n<head><title>413 Request Entity Too Large</title></head>\r\n<body>\r\n<center><h1>413 Request Entity Too Large</h1></center>\r\n<hr><center>nginx</center>\r\n</body>\r\n</html>\r\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case rearm.TokenPath:
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"access_token":"t-1","token_type":"Bearer","expires_in":3600}`)
		case rearm.ProgrammaticPath:
			read, _ := io.Copy(io.Discard, r.Body)
			mu.Lock()
			uploads++
			seenSize = r.ContentLength
			mu.Unlock()
			if read != r.ContentLength {
				t.Errorf("the server read %d bytes, Content-Length %d", read, r.ContentLength)
			}
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			_, _ = io.WriteString(w, page)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	rearmUri, apiKeyId, apiKey = srv.URL, "FREEFORM__o__ord__1", "secret"
	apiClient = nil

	report := &sessionReport{body: bytes.Repeat([]byte("x"), 3*1048576), filename: "orientation.md", source: "orientation.md"}
	err := uploadSessionReport("s-1", report, reportOrientation)
	if err == nil {
		t.Fatal("a 413 answer is an error")
	}
	mu.Lock()
	sent, seen := uploads, seenSize
	mu.Unlock()
	if sent != 1 {
		t.Fatalf("the upload was sent %d times, want 1", sent)
	}
	if seen <= 3*1048576 {
		t.Fatalf("the server saw Content-Length %d, want the report and the multipart envelope", seen)
	}
	size := fmt.Sprintf("%.1f MB", float64(seen)/1048576)
	got := describeError(err)
	if !strings.HasPrefix(got, "the request of ") || !strings.Contains(got, "the request of "+size+" exceeds") {
		t.Fatalf("the message names the size the server saw (%s, %s bytes): %q", size, strconv.FormatInt(seen, 10), got)
	}
	if strings.Contains(got, "<html") {
		t.Fatalf("the page is not printed: %q", got)
	}
	// and the same call as upload commands make it
	_, err = rearm.UploadMultipart(context.Background(), rearmClient(), "x", "mutation x { y }", map[string]interface{}{"f": nil},
		[]rearm.FilePart{{Filename: "f", Content: bytes.NewReader([]byte("abc")), VariablePath: "variables.f"}})
	if msg, ok := uploadLimitMessage(err, true); !ok || !strings.HasSuffix(msg, batchHint) {
		t.Fatalf("got (%q, %v)", msg, ok)
	}
}

// Who answered a 413: the start of its page names the layer whose limit was hit.
func TestUploadLimitMessageNamesWhoAnswered(t *testing.T) {
	const rearmPage = "Your request has returned an error. You may contact us at i&#110;fo&#64;r&#101;&#108;iza&#46;&#105;o."
	const nginxPage = "<html>\r\n<head><title>413 Request Entity Too Large</title></head>\r\n<body>\r\n<center><h1>413 Request Entity Too Large</h1></center>\r\n<hr><center>nginx</center>\r\n</body>\r\n</html>"
	const gatewayPage = "<html>\r\n<head><title>413 Request Entity Too Large</title></head>\r\n<body>\r\n<center><h1>413 Request Entity Too Large</h1></center>\r\n<hr><center>Microsoft-Azure-Application-Gateway/v2</center>\r\n</body>\r\n</html>"
	long := strings.Repeat("abcdefghij ", 20)
	cases := []struct {
		name, body, want string
	}{
		{"ReARM's own page, entities decoded", rearmPage,
			" It was answered by the ReARM web server (nginx), whose limit is that Helm value."},
		{"Traefik's plain text", "Request Entity Too Large",
			` It was answered by something in front of ReARM, which has a limit of its own; its answer begins: "Request Entity Too Large".`},
		{"another nginx, tags removed", nginxPage,
			` It was answered by something in front of ReARM, which has a limit of its own; its answer begins: "413 Request Entity Too Large 413 Request Entity Too Large nginx".`},
		{"a cloud gateway names itself", gatewayPage,
			` It was answered by something in front of ReARM, which has a limit of its own; its answer begins: "413 Request Entity Too Large 413 Request Entity Too Large Microsoft-Azure-Application-Gateway/v2".`},
		{"a long answer is cut", long,
			` It was answered by something in front of ReARM, which has a limit of its own; its answer begins: "` + strings.TrimSpace(strings.TrimSpace(long)[:120]) + `...".`},
		{"no page", "", ""},
		{"only markup", "<html><body></body></html>", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := &rearm.StatusError{StatusCode: 413, RequestBytes: 34288435, Body: c.body}
			msg, ok := uploadLimitMessage(err, true)
			if !ok {
				t.Fatal("a 413 is rewritten")
			}
			if want := "the request of 32.7 MB" + uploadLimitTail + c.want + batchHint; msg != want {
				t.Fatalf("got  %q\nwant %q", msg, want)
			}
			if strings.Contains(msg, "<") {
				t.Fatalf("no markup is printed: %q", msg)
			}
		})
	}
}
