package asr

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

func TestASRProxyAndSSRFGuards(t *testing.T) {
	if os.Getenv("WEKNORA_ASR_PROXY_PROBE") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestASRProxyAndSSRFGuards$")
		cmd.Env = append(os.Environ(), "WEKNORA_ASR_PROXY_PROBE=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
		return
	}
	var connects atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			connects.Add(1)
		}
		w.WriteHeader(502)
	}))
	defer proxy.Close()
	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")
	t.Setenv("SSRF_WHITELIST", "asr-proxy.test")
	client := newASRHTTPClient(time.Second)
	_, err := client.Get("https://asr-proxy.test/v1/audio")
	if err == nil || connects.Load() != 1 {
		t.Fatalf("proxy connect=%d err=%v", connects.Load(), err)
	}
	_, err = client.Get("http://169.254.169.254/latest/meta-data")
	if err == nil || !strings.Contains(err.Error(), "SSRF") {
		t.Fatalf("metadata bypass %v", err)
	}
}

func TestASRNoProxyHonorsNoProxy(t *testing.T) {
	if os.Getenv("WEKNORA_ASR_NO_PROXY_PROBE") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestASRNoProxyHonorsNoProxy$")
		cmd.Env = append(os.Environ(), "WEKNORA_ASR_NO_PROXY_PROBE=1", "HTTPS_PROXY=http://127.0.0.1:9", "NO_PROXY=asr-noproxy.test")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
		return
	}
	u, _ := url.Parse("https://asr-noproxy.test/x")
	p, err := sharedASRHTTPTransport.Proxy(&http.Request{URL: u})
	if err != nil || p != nil {
		t.Fatalf("NO_PROXY ignored: %v %v", p, err)
	}
}
