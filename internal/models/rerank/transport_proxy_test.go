package rerank

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRerankProxyAndSSRFGuards(t *testing.T) {
	if os.Getenv("WEKNORA_RERANK_PROXY_PROBE") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestRerankProxyAndSSRFGuards$")
		cmd.Env = append(os.Environ(), "WEKNORA_RERANK_PROXY_PROBE=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
		return
	}
	var n atomic.Int32
	p := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			n.Add(1)
		}
		w.WriteHeader(502)
	}))
	defer p.Close()
	t.Setenv("HTTPS_PROXY", p.URL)
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")
	t.Setenv("SSRF_WHITELIST", "rerank-proxy.test")
	c := newRerankHTTPClient(time.Second)
	_, err := c.Get("https://rerank-proxy.test/x")
	if err == nil || n.Load() != 1 {
		t.Fatalf("proxy=%d err=%v", n.Load(), err)
	}
	_, err = c.Get("http://169.254.169.254/latest/meta-data")
	if err == nil || !strings.Contains(err.Error(), "SSRF") {
		t.Fatal(err)
	}
}

func TestRerankNoProxyHonorsNoProxy(t *testing.T) {
	if os.Getenv("WEKNORA_RERANK_NO_PROXY_PROBE") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestRerankNoProxyHonorsNoProxy$")
		cmd.Env = append(os.Environ(), "WEKNORA_RERANK_NO_PROXY_PROBE=1", "HTTPS_PROXY=http://127.0.0.1:9", "NO_PROXY=rerank-noproxy.test")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
		return
	}
	u, _ := url.Parse("https://rerank-noproxy.test/x")
	p, err := sharedRerankHTTPTransport.Proxy(&http.Request{URL: u})
	if err != nil || p != nil {
		t.Fatalf("%v %v", p, err)
	}
}
