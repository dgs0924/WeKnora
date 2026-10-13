package asr

import (
	"fmt"
	"net/http"
	"time"

	secutils "github.com/Tencent/WeKnora/internal/utils"
)

func validateASRBaseURL(baseURL string) error {
	if baseURL == "" {
		return nil
	}
	if err := secutils.ValidateURLForSSRF(baseURL); err != nil {
		return fmt.Errorf("base URL SSRF check failed: %w", err)
	}
	return nil
}

func newASRHTTPClient(timeout time.Duration) *http.Client {
	cfg := secutils.DefaultSSRFSafeHTTPClientConfig()
	cfg.Timeout = timeout
	return secutils.NewSSRFSafeHTTPClientWithTransport(cfg, sharedASRHTTPTransport)
}

var sharedASRHTTPTransport = func() *http.Transport {
	transport := secutils.NewSSRFSafeTransport(secutils.DefaultSSRFSafeHTTPClientConfig())
	transport.Proxy = http.ProxyFromEnvironment
	return transport
}()
