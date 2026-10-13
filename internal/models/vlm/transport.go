package vlm

import (
	"fmt"
	"net/http"
	"time"

	secutils "github.com/Tencent/WeKnora/internal/utils"
)

func validateVLMBaseURL(baseURL string) error {
	if baseURL == "" {
		return nil
	}
	if err := secutils.ValidateURLForSSRF(baseURL); err != nil {
		return fmt.Errorf("base URL SSRF check failed: %w", err)
	}
	return nil
}

func newVLMHTTPClient(timeout time.Duration) *http.Client {
	cfg := secutils.DefaultSSRFSafeHTTPClientConfig()
	cfg.Timeout = timeout
	return secutils.NewSSRFSafeHTTPClientWithTransport(cfg, sharedVLMHTTPTransport)
}

var sharedVLMHTTPTransport = func() *http.Transport {
	transport := secutils.NewSSRFSafeTransport(secutils.DefaultSSRFSafeHTTPClientConfig())
	transport.Proxy = http.ProxyFromEnvironment
	return transport
}()
