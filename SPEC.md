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
    -   Matches normalized hostnames exactly or at a subdomain boundary; the longest matching route wins.
    -   Proxies every HTTP method to the target backend, preserving paths, queries, response statuses, and streaming.
    -   Forwards end-to-end headers and generates client-address metadata from the incoming connection.
    -   Logs access in Common Log Format (CLF).

### Authentigate
-   **Role**: Authentication Provider, Gateway.
-   **Tech Stack**: Go (stdlib `net/http`), `gocialite` (OAuth2), `gotp` (TOTP), `gokv` (KV store).
-   **Logic**:
    -   Manages Users (Session Token, OAuth Foreign IDs, TOTP Secrets).
    -   Provides routes for Login (OAuth2 callbacks), User Management, and Token display.
    -   Session tokens contain 32 cryptographically random bytes. Rotating a token revokes the previous token without changing the user's authenticator secret. Session lookup requires an existing user whose current token matches the request; development identities cannot authenticate when development mode is disabled.
    -   Account changes require POST forms with the current session token as a security token. Cookie-authenticated changes must originate from the configured origin. HTTPS cookies use the `__Host-` prefix, root path, Secure, HttpOnly, and SameSite=Lax. Account pages escape user text, embed enrollment QR images, and prevent caching and referrer disclosure.
    -   OAuth login binds signed, expiring state to its initiating browser and provider. Provider configuration is validated at startup when OAuth is enabled. Authentication failures redact session tokens, OAuth codes and state, cookies, and provider secrets from logs.
    -   Authenticator login works without an existing session. Accepted time steps cannot be reused; failures are limited to five per account and twenty per source address in five minutes. Account failure counts and the last accepted step persist with the user. Address tracking holds at most 4096 entries.
    -   Authenticator signup displays enrollment once and saves an unapproved entry in `totp_pending.json`. Login displays the pending-approval page until approval. An approved entry still requires a valid authenticator code; successful login creates a full user and protected session, preserves the enrolled secret, removes the pending entry, and records the accepted step to reject replay.
    -   **Gateway Middleware**: `makeAuthedRelay` checks for session validity.
        -   If valid: Forwards request to configured microservice, injecting `authentigate-id`, `authentigate-token` headers.
        -   If invalid: Redirects to login or shows error.
    -   **Routing**: Uses `http.ServeMux` with Go 1.22+ patterns (`/path/{param}`), using a custom lightweight `Context` wrapper for handler convenience.

## Design Decisions & Quirks

### Vhoster
1.  **Proxying**: Preserves end-to-end HTTP behavior, repeated response headers, and streaming. Upstream failures return HTTP 502.
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

Authenticator QR images are embedded in authenticated account pages. The public QR-file route is not served, including for files created by earlier versions, so a revoked token cannot retrieve an authenticator secret through an old QR-image URL.

Session and provider mappings store user IDs as JSON strings. Storage failures during rotation are reported as errors instead of successful account updates. Within a gateway process, token rotation and account management use one consistent view of the user record.

Application diagnostics omit session tokens, authenticator secrets and provisioning URLs, OAuth credentials, request cookies and authorization headers, request/response payloads, and credential-bearing URLs. Request logs identify the method, registered route, user or upstream host, and outcome where available. Error logs retain the failure cause and upstream status while redacting credentials. Public response bodies and normal proxy forwarding retain their existing behavior.

## WebSocket Forwarding

WebSocket requests preserve the configured upstream path and query together with the client's escaped route suffix and query. HTTP upstream URLs use WebSocket transport; HTTPS upstream URLs use TLS-protected WebSocket transport. The handshake forwards the gateway-supplied identity headers and configured request headers. Requested subprotocols reach the upstream server, and the selected upstream subprotocol is returned to the browser.

The upstream handshake completes before the browser connection is accepted. An upstream handshake failure returns HTTP 502 and logs its status and cause without request credentials. Text and binary messages are streamed in both directions without logging their contents. When either direction ends, both connections close. Browser origins must match the requested gateway host.

## Camera Streaming

Camera viewers share one upstream connection per active camera. A viewer is registered before a new upstream connection starts. The gateway waits for the camera's response headers, requires HTTP 200 with a valid `multipart/x-mixed-replace` Content-Type and boundary, and passes that Content-Type to viewers unchanged. Connection failures, unexpected status codes, and invalid stream headers return HTTP 502 with the reason and camera identity.

Each viewer has a bounded queue. A viewer whose queue fills is removed from the stream and disconnected, including when its response is blocked writing to the network. The viewer receives an uninterrupted prefix of the stream up to disconnection; bytes are never discarded from the middle of a continuing stream. Other viewers continue receiving the shared stream. When the final viewer leaves, the upstream connection closes.

## Local Verification

`build.sh` builds the gateway, demonstration applications, vhoster, and camera module. `test.sh` runs every module's regression tests with race detection and static checks. Regression tests use local HTTP/TLS/WebSocket servers and temporary storage; external OAuth services and cameras are not required.

## Menu and presence demonstrations

The menu uses the gateway identity for stored messages and images. An account with no stored message receives the sentinel none; a one-byte message is retained. Multipart uploads reject missing/invalid image data and excessive sizes. Images are served with their validated media type and browser content sniffing disabled. Account links lead to the gateway's account controls.

Presence rooms store named participants, last update time, external address, and local addresses. Polling retains the room ID. Participant names and addresses are escaped when rendered, and values embedded in script are serialized as JSON. The presence client parses interface addresses with network prefixes, encodes query parameters, bounds request duration, and avoids printing credential URLs.

The menu store uses the maintained Bolt-compatible storage library. The module requires Go 1.25 or later. Menu data files retain their existing format.

## Authenticated request relay

The relay combines the upstream path prefix with the captured API path and preserves escaped path bytes and query parameters. It forwards selected request headers, removes hop-by-hop headers, and replaces identity and client-address metadata with gateway-generated values. The built-in request headers include content type, content disposition, accept, and accept language. Request content length matches the buffered body.

The gateway supplies `authentigate-id`, `authentigate-token`, `authentigate-base-url`, `authentigate-base-token-url`, and `authentigate-top-url`. They identify the authenticated user, current session, cookie-based service URL, token-based service URL, and authenticated site URL, respectively. Client-supplied versions never override these values.

HTTP upstream status codes and end-to-end response headers are preserved, including multiple cookies. Redirects are returned to the caller instead of followed with gateway credentials. Requests exceeding the body limit return 413. Upstream connection/read failures return 502; upstream timeouts return 504. Oversized upstream responses return 502. Request cancellation is propagated to the upstream operation. Response bodies are closed after use.

WebSocket connections preserve the upstream URL query, authenticated headers, and negotiated subprotocol. HTTPS upstreams use secure WebSockets. Browser origins must match the requested gateway host. The upstream handshake succeeds before the client connection is upgraded. Both directions enforce the configured message limit and close when either direction ends.

Access logs append through a direct file writer.


The body limit defaults to 33554432 bytes and is configured with MaxBodyBytes in config.json. UpstreamTimeoutSeconds defaults to 30 seconds and bounds buffered HTTP requests, WebSocket handshakes, and WebSocket writes. Multipart camera streams continue after their response headers arrive and end when the upstream or caller closes. A stream is not buffered or limited to the buffered-response timeout. Invalid configuration is rejected at startup; CopyHeaders is an array of header names.


## Hostname routing contract

Vhoster reads its own `config.json`. Each redirect requires `Host` and an absolute HTTP or HTTPS `To`; `Name` is descriptive. `HostNames` selects certificate names, and `LogFile` defaults to `accessLog`. `Port` and `CopyHeaders` remain accepted compatibility fields; development listens on port 3000 and end-to-end headers are forwarded.

Hostname matching ignores case, a trailing dot, and the request port. A route matches its exact hostname or a subdomain separated by a dot. IP-address routes match exactly. The most specific matching hostname wins. Unmatched hosts return 404.

All HTTP methods are relayed. Escaped paths, query parameters, response status, repeated headers, and streaming responses are preserved. Redirects are returned without following them. Forwarded client metadata is generated from the incoming connection, and upstream failures return 502. Logging is safe under concurrent requests.


The -c or -config flag selects configuration. The -l or -log flag overrides the configured LogFile, and LOG_FILE overrides both. Access logs append to existing files.

## Build and test commands

The root Go module requires Go 1.25 or later. Dependencies are pinned by each module's go.mod and go.sum files.

build.sh builds the root module, vhoster, cameraview, and both shopping modules. test.sh runs every module's tests with race detection and then go vet. Tests use local handlers and temporary stores.

run.sh builds Authentigate, starts it in development mode, and starts the demonstration modules. modules/run.sh builds menu and presence before starting them and the shopping launcher. Launchers stop at a build failure and retain existing binaries until their replacements build successfully. Generated binaries are ignored by Git.
