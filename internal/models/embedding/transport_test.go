package embedding

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
)

func TestEmbeddingUsesConfiguredHTTPSProxy(t *testing.T) {
	// ProxyFromEnvironment caches process environment; use a fresh process so
	// this regression test does not depend on other tests' outbound requests.
	if os.Getenv("WEKNORA_EMBEDDING_PROXY_PROBE") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestEmbeddingUsesConfiguredHTTPSProxy$")
		cmd.Env = append(os.Environ(), "WEKNORA_EMBEDDING_PROXY_PROBE=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("proxy regression: %v\n%s", err, output)
		}
		return
	}
	var connects atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect && r.Host == "embedding-proxy.test:443" {
			connects.Add(1)
		}
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer proxy.Close()
	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")
	t.Setenv("SSRF_WHITELIST", "embedding-proxy.test")
	client := newEmbeddingHTTPClient(3 * time.Second)
	_, err := client.Get("https://embedding-proxy.test/v1/embeddings")
	if err == nil {
		t.Fatal("test proxy deliberately rejects the tunnel")
	}
	if connects.Load() != 1 {
		t.Fatalf("expected CONNECT through configured proxy; got %d, err=%v", connects.Load(), err)
	}
	_, err = client.Get("http://169.254.169.254/latest/meta-data")
	if err == nil || !strings.Contains(err.Error(), "SSRF") {
		t.Fatalf("proxy must not bypass target validation: %v", err)
	}
}

func TestNewEmbeddingHTTPClient_ReusesTransport(t *testing.T) {
	firstTimeout := 15 * time.Second
	secondTimeout := 45 * time.Second
	first := newEmbeddingHTTPClient(firstTimeout)
	second := newEmbeddingHTTPClient(secondTimeout)

	if first == second {
		t.Fatal("expected distinct HTTP clients")
	}
	firstGuard, ok := first.Transport.(*secutils.SSRFValidatingRoundTripper)
	if !ok {
		t.Fatalf("expected SSRF-validating transport, got %T", first.Transport)
	}
	secondGuard, ok := second.Transport.(*secutils.SSRFValidatingRoundTripper)
	if !ok {
		t.Fatalf("expected SSRF-validating transport, got %T", second.Transport)
	}
	if firstGuard.Base != secondGuard.Base {
		t.Fatal("expected embedding HTTP clients to share a base transport")
	}
	if firstGuard.Base != http.RoundTripper(sharedEmbeddingHTTPTransport) {
		t.Fatal("expected embedding HTTP client to use the shared transport")
	}
	if first.Timeout != firstTimeout {
		t.Fatalf("unexpected first client timeout: got %v, want %v", first.Timeout, firstTimeout)
	}
	if second.Timeout != secondTimeout {
		t.Fatalf("unexpected second client timeout: got %v, want %v", second.Timeout, secondTimeout)
	}
}

func TestValidateEmbeddingBaseURL_RejectsLoopback(t *testing.T) {
	err := validateEmbeddingBaseURL("http://169.254.169.254/latest/meta-data")
	if err == nil {
		t.Fatal("expected SSRF error for link-local metadata URL")
	}
	if !strings.Contains(err.Error(), "SSRF") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateEmbeddingBaseURL_AllowsEmpty(t *testing.T) {
	if err := validateEmbeddingBaseURL(""); err != nil {
		t.Fatalf("empty base URL should be allowed: %v", err)
	}
}

func TestNewEmbedder_RejectsPrivateBaseURL(t *testing.T) {
	_, err := NewEmbedder(Config{
		Source:    types.ModelSourceRemote,
		Provider:  "openai",
		BaseURL:   "http://169.254.169.254/latest/meta-data",
		ModelName: "text-embedding-3-small",
		APIKey:    "test-key",
	}, nil, nil)
	if err == nil {
		t.Fatal("expected SSRF rejection for link-local metadata URL")
	}
}
