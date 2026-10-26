# Project Specification: Vhoster & Authentigate

## Overview
This project consists of two main components designed to facilitate running multiple websites and products behind a single entry point.

1.  **Vhoster**: The edge server/reverse proxy. It handles SSL/TLS termination, domain-based routing, and forwards requests to the appropriate backend service.
2.  **Authentigate**: An identity management and authentication gateway. It acts as an upstream for Vhoster for protected routes, handling user login (OAuth2, TOTP), session management, and forwarding authenticated requests to other microservices.

## Architecture & Implementation

### Vhoster
-   **Role**: SSL Termination, Reverse Proxy.
-   **Tech Stack**: Go (stdlib `net/http`), `autocert` for Let's Encrypt.
-   **Configuration**: `config.json` defines routes (`Redirects`), mapping Hostnames to target URLs.
-   **Logic**:
    -   Listens on port 80/443.
    -   Matches `Host` header against configured suffixes.
    -   Manually proxies requests (GET, POST, PUT) to the target backend.
    -   Copies headers manually from client request to backend and vice versa.
    -   Logs access in Common Log Format (CLF).

### Authentigate
-   **Role**: Authentication Provider, Gateway.
-   **Tech Stack**: Go (stdlib `net/http`), `gocialite` (OAuth2), `gotp` (TOTP), `gokv` (KV store).
-   **Logic**:
    -   Manages Users (Session Token, OAuth Foreign IDs, TOTP Secrets).
    -   Provides routes for Login (OAuth2 callbacks), User Management, and Token display.
    -   **Gateway Middleware**: `makeAuthedRelay` checks for session validity.
        -   If valid: Forwards request to configured microservice, injecting `authentigate-id`, `authentigate-token` headers.
        -   If invalid: Redirects to login or shows error.
    -   **Routing**: Uses `http.ServeMux` with Go 1.22+ patterns (`/path/{param}`), using a custom lightweight `Context` wrapper for handler convenience.

## Design Decisions & Quirks

### Vhoster
1.  **Manual Proxying**: Uses manual `client.Do(req)` instead of `httputil.ReverseProxy` to maintain precise control over headers and behavior.
2.  **Custom Mux**: Implements a `MyMux` struct for host-aware routing logic that standard `http.ServeMux` does not provide.
3.  **Panic Handling**: Relies on specific panic handling patterns for error management in handling routines.

### Authentigate
1.  **Context Wrapper**: Features a minimal `Context` struct wrapping `http.ResponseWriter` and `*http.Request` to provide convenient helper methods (`Param`, `JSON`, `Query`) similar to web frameworks, while sticking to the standard library.
2.  **String-Replace Templating**: Uses simple string replacement for templating rather than `html/template`, by design.
3.  **Manual Session ID Generation**: Generates session tokens using `r.Int()`.
4.  **Implicit Cookie/Token Logic**: Utilizes a mixed strategy of Cookie-based and URL-param-based tokens to support revocable access.
5.  **Hardcoded File Paths**: Expects specific file locations (e.g., `files/BackendFailure.html`) relative to the working directory.

## Authenticated request relay

The relay combines the upstream path prefix with the captured API path and preserves escaped path bytes and query parameters. It forwards selected request headers, removes hop-by-hop headers, and replaces identity and client-address metadata with gateway-generated values. The built-in request headers include content type, content disposition, accept, and accept language. Request content length matches the buffered body.

The gateway supplies `authentigate-id`, `authentigate-token`, `authentigate-base-url`, `authentigate-base-token-url`, and `authentigate-top-url`. They identify the authenticated user, current session, cookie-based service URL, token-based service URL, and authenticated site URL, respectively. Client-supplied versions never override these values.

HTTP upstream status codes and end-to-end response headers are preserved, including multiple cookies. Redirects are returned to the caller instead of followed with gateway credentials. Requests exceeding the body limit return 413. Upstream connection/read failures return 502; upstream timeouts return 504. Oversized upstream responses return 502. Request cancellation is propagated to the upstream operation. Response bodies are closed after use.

WebSocket connections preserve the upstream URL query, authenticated headers, and negotiated subprotocol. HTTPS upstreams use secure WebSockets. Browser origins must match the requested gateway host. The upstream handshake succeeds before the client connection is upgraded. Both directions enforce the configured message limit and close when either direction ends.

Access logs append through a direct file writer.


The body limit defaults to 33554432 bytes and is configured with MaxBodyBytes in config.json. UpstreamTimeoutSeconds defaults to 30 seconds and bounds buffered HTTP requests, WebSocket handshakes, and WebSocket writes. Multipart camera streams continue after their response headers arrive and end when the upstream or caller closes. A stream is not buffered or limited to the buffered-response timeout. Invalid configuration is rejected at startup; CopyHeaders is an array of header names.
