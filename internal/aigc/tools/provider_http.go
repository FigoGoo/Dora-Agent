package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

const (
	maxProviderJSONBytes = int64(128 << 20)
	maxImageAssetBytes   = int64(25 << 20)
	maxVideoAssetBytes   = int64(512 << 20)
)

func decodeLimitedProviderJSON(body io.Reader, limit int64, target any) error {
	limited := &io.LimitedReader{R: body, N: limit + 1}
	if err := json.NewDecoder(limited).Decode(target); err != nil {
		return err
	}
	if limited.N <= 0 {
		return fmt.Errorf("provider response exceeds %d bytes", limit)
	}
	return nil
}

func downloadProviderObject(ctx context.Context, client *http.Client, rawURL, providerEndpoint string, maxBytes int64) ([]byte, string, error) {
	parsed, err := validateProviderObjectURL(ctx, rawURL, providerEndpoint)
	if err != nil {
		return nil, "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, "", err
	}
	if client == nil {
		client = http.DefaultClient
	}
	safeClient := *client
	previousRedirect := client.CheckRedirect
	safeClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("provider download exceeded redirect limit")
		}
		if _, err := validateProviderObjectURL(req.Context(), req.URL.String(), providerEndpoint); err != nil {
			return err
		}
		if previousRedirect != nil {
			return previousRedirect(req, via)
		}
		return nil
	}
	response, err := safeClient.Do(request)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return nil, "", fmt.Errorf("provider object returned %s: %s", response.Status, strings.TrimSpace(string(raw)))
	}
	if response.ContentLength > maxBytes {
		return nil, "", fmt.Errorf("provider object exceeds %d bytes", maxBytes)
	}
	limited := io.LimitReader(response.Body, maxBytes+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return nil, "", err
	}
	if int64(len(raw)) > maxBytes {
		return nil, "", fmt.Errorf("provider object exceeds %d bytes", maxBytes)
	}
	return raw, strings.TrimSpace(response.Header.Get("Content-Type")), nil
}

func validateProviderObjectURL(ctx context.Context, rawURL, providerEndpoint string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Hostname() == "" {
		return nil, fmt.Errorf("invalid provider object url")
	}
	endpoint, _ := url.Parse(strings.TrimSpace(providerEndpoint))
	allowLoopbackHTTP := endpoint != nil && endpoint.Scheme == "http" && isLoopbackHost(endpoint.Hostname())
	if parsed.Scheme != "https" && !(allowLoopbackHTTP && parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname())) {
		return nil, fmt.Errorf("provider object url must use https")
	}
	if allowLoopbackHTTP && isLoopbackHost(parsed.Hostname()) {
		return parsed, nil
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, parsed.Hostname())
	if err != nil || len(addresses) == 0 {
		return nil, fmt.Errorf("resolve provider object host: %w", err)
	}
	for _, address := range addresses {
		ip := address.IP
		if ip.IsPrivate() || ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return nil, fmt.Errorf("provider object host resolves to a non-public address")
		}
	}
	return parsed, nil
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
