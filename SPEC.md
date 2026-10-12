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
3.  **Session Tokens**: Each new token contains 32 cryptographically random bytes, encoded as 64 lowercase hexadecimal characters. A session mapping is valid only when its token matches the current token in the saved user record.
4.  **Implicit Cookie/Token Logic**: Utilizes a mixed strategy of Cookie-based and URL-param-based tokens to support revocable access.
5.  **Hardcoded File Paths**: Expects specific file locations (e.g., `files/BackendFailure.html`) relative to the working directory.

## Token Rotation and Diagnostics

Rotating a session token saves the new mapping and current user record, removes the old mapping, and updates the browser's session cookie and account page together. The username and authenticator enrollment remain unchanged. An old mapping cannot authenticate after rotation even if removing it from storage fails. Account management requests recheck their presented token before making changes, so a request authenticated before rotation cannot subsequently update the account with the revoked token. Existing tokens remain usable until rotated, provided they match their saved user record.

Session and provider mappings store user IDs as JSON strings. Storage failures during rotation are reported as errors instead of successful account updates. Within a gateway process, token rotation and account management use one consistent view of the user record.

Application diagnostics omit session tokens, authenticator secrets and provisioning URLs, OAuth credentials, request cookies and authorization headers, request/response payloads, and credential-bearing URLs. Request logs identify the method, registered route, user or upstream host, and outcome where available. Error logs retain the failure cause and upstream status while redacting credentials. Public response bodies and normal proxy forwarding retain their existing behavior.

## WebSocket Forwarding

WebSocket requests preserve the configured upstream path and query together with the client's escaped route suffix and query. HTTP upstream URLs use WebSocket transport; HTTPS upstream URLs use TLS-protected WebSocket transport. The handshake forwards the gateway-supplied identity headers and configured request headers. Requested subprotocols reach the upstream server, and the selected upstream subprotocol is returned to the browser.

The upstream handshake completes before the browser connection is accepted. An upstream handshake failure returns HTTP 502 and logs its status and cause without request credentials. Text and binary messages are streamed in both directions without logging their contents. When either direction ends, both connections close. The existing browser-origin policy is retained.

## Camera Streaming

Camera viewers share one upstream connection per active camera. A viewer is registered before a new upstream connection starts. The gateway waits for the camera's response headers, requires HTTP 200 with a valid `multipart/x-mixed-replace` Content-Type and boundary, and passes that Content-Type to viewers unchanged. Connection failures, unexpected status codes, and invalid stream headers return HTTP 502 with the reason and camera identity.

Each viewer has a bounded queue. A viewer whose queue fills is removed from the stream and receives only its already-queued, uninterrupted prefix followed by the end of the response. Bytes are never discarded from the middle of a continuing viewer stream. Other viewers continue receiving the shared stream. When the final viewer leaves, the upstream connection closes.

## Local Verification

`build.sh` builds the gateway, demonstration applications, vhoster, and camera module. `test.sh` runs every module's regression tests with race detection and static checks. Regression tests use local HTTP/TLS/WebSocket servers and temporary storage; external OAuth services and cameras are not required.
