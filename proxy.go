package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/donomii/gin"
)

const defaultMaxBodyBytes int64 = 32 << 20
const defaultUpstreamTimeoutSeconds = 30

var errRelayBodyTooLarge = errors.New("body exceeds the configured limit")

func relayBodyLimit() int64 {
	if config == nil || config.MaxBodyBytes == 0 {
		return defaultMaxBodyBytes
	}
	return config.MaxBodyBytes
}

func relayTimeout() time.Duration {
	if config == nil || config.UpstreamTimeoutSeconds == 0 {
		return defaultUpstreamTimeoutSeconds * time.Second
	}
	return time.Duration(config.UpstreamTimeoutSeconds) * time.Second
}

func readRelayBody(body io.Reader) ([]byte, error) {
	limit := relayBodyLimit()
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w of %d bytes", errRelayBodyTooLarge, limit)
	}
	return data, nil
}

func upstreamErrorCause(err error) error {
	var requestError *url.Error
	if errors.As(err, &requestError) {
		return requestError.Err
	}
	return err
}

func relayURL(c *gin.Context, relay *Redirect) (*url.URL, error) {
	target, err := url.Parse(relay.To)
	if err != nil {
		return nil, err
	}
	if target.Hostname() == "" || (target.Scheme != "http" && target.Scheme != "https") {
		return nil, fmt.Errorf("route %q requires an absolute HTTP or HTTPS upstream URL", relay.Name)
	}
	api := c.Param("api")
	if api != "" {
		escapedPath := c.Request.URL.EscapedPath()
		prefixLength := len(c.Request.URL.Path) - len(api)
		if prefixLength < 0 || !strings.HasSuffix(c.Request.URL.Path, api) {
			return nil, fmt.Errorf("route %q API path does not match the requested path", relay.Name)
		}
		encodedPrefixLength := 0
		for index := 0; index < prefixLength; index++ {
			if escapedPath[encodedPrefixLength] == '%' {
				encodedPrefixLength += 3
			} else {
				encodedPrefixLength++
			}
		}
		basePath, baseEscapedPath := target.Path, target.EscapedPath()
		if strings.HasSuffix(baseEscapedPath, "/") {
			basePath = strings.TrimSuffix(basePath, "/")
			baseEscapedPath = strings.TrimSuffix(baseEscapedPath, "/")
		}
		target.RawPath = baseEscapedPath + escapedPath[encodedPrefixLength:]
		target.Path = basePath + api
	}
	if c.Request.URL.RawQuery != "" {
		if target.RawQuery != "" {
			target.RawQuery += "&" + c.Request.URL.RawQuery
		} else {
			target.RawQuery = c.Request.URL.RawQuery
		}
	}
	return target, nil
}

func removeHopHeaders(header http.Header) {
	for _, connection := range header.Values("Connection") {
		for _, name := range strings.Split(connection, ",") {
			header.Del(strings.TrimSpace(name))
		}
	}
	for _, name := range []string{"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade"} {
		header.Del(name)
	}
}

func relayRequest(c *gin.Context, id, token string, relay *Redirect, useCookie bool, body []byte) (*http.Request, error) {
	target, err := relayURL(c, relay)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, target.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	headers := append([]string{"Content-Type", "Content-Disposition", "Accept", "Accept-Language"}, relay.CopyHeaders...)
	sourceHeaders := c.Request.Header.Clone()
	removeHopHeaders(sourceHeaders)
	copied := make(map[string]bool)
	for _, name := range headers {
		name = http.CanonicalHeaderKey(name)
		if strings.HasPrefix(strings.ToLower(name), "authentigate-") || copied[name] {
			continue
		}
		copied[name] = true
		for _, value := range sourceHeaders.Values(name) {
			req.Header.Add(name, value)
		}
	}
	removeHopHeaders(req.Header)
	req.Header.Del("Content-Length")
	req.Header.Del("Forwarded")
	AddAuthToRequest(req, id, token, baseUrl, relay, useCookie)
	address, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		address = c.Request.RemoteAddr
	}
	req.Header.Set("X-Forwarded-For", address)
	req.Header.Set("X-Real-IP", address)
	req.Header.Set("X-Forwarded-Host", c.Request.Host)
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	req.Header.Set("X-Forwarded-Proto", scheme)
	port := "80"
	if scheme == "https" {
		port = "443"
	}
	if _, requestPort, err := net.SplitHostPort(c.Request.Host); err == nil {
		port = requestPort
	}
	req.Header.Set("X-Forwarded-Port", port)
	return req, nil
}

func relayHTTPHandler(c *gin.Context, id, token string, relay *Redirect, useCookie bool) {
	body, err := readRelayBody(c.Request.Body)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errRelayBodyTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		c.String(status, "Cannot relay request to %s: %v", relay.Name, err)
		return
	}
	req, err := relayRequest(c, id, token, relay, useCookie, body)
	if err != nil {
		c.String(http.StatusBadGateway, "Cannot prepare request for upstream %s: %v", relay.Name, err)
		return
	}
	if strings.EqualFold(c.Request.Header.Get("Upgrade"), "websocket") {
		upgradeAndHandle(c, req)
		return
	}
	client := &http.Client{
		Timeout: relayTimeout(),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		status := http.StatusBadGateway
		var timeout net.Error
		if errors.As(err, &timeout) && timeout.Timeout() {
			status = http.StatusGatewayTimeout
		}
		logUpstreamFailure(relay.Name, req.URL.Host, err)
		c.String(status, "Request to upstream %s (%s) failed: %v", relay.Name, req.URL.Host, upstreamErrorCause(err))
		return
	}
	defer resp.Body.Close()
	responseBody, err := readRelayBody(resp.Body)
	if err != nil {
		status := http.StatusBadGateway
		var timeout net.Error
		if errors.As(err, &timeout) && timeout.Timeout() {
			status = http.StatusGatewayTimeout
		}
		c.String(status, "Cannot read response from upstream %s: %v", relay.Name, err)
		return
	}
	removeHopHeaders(resp.Header)
	for name, values := range resp.Header {
		for _, value := range values {
			c.Writer.Header().Add(name, value)
		}
	}
	c.Status(resp.StatusCode)
	c.Writer.Write(responseBody)
	if accessLog != nil {
		fmt.Fprintln(accessLog, format_clf(c, id, fmt.Sprint(resp.StatusCode), fmt.Sprint(len(responseBody))))
	}
}
