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
3.  **Manual Session ID Generation**: Generates session tokens using `r.Int()`.
4.  **Implicit Cookie/Token Logic**: Utilizes a mixed strategy of Cookie-based and URL-param-based tokens to support revocable access.
5.  **Hardcoded File Paths**: Expects specific file locations (e.g., `files/BackendFailure.html`) relative to the working directory.

## Hostname routing contract

Vhoster reads its own `config.json`. Each redirect requires `Host` and an absolute HTTP or HTTPS `To`; `Name` is descriptive. `HostNames` selects certificate names, and `LogFile` defaults to `accessLog`. `Port` and `CopyHeaders` remain accepted compatibility fields; development listens on port 3000 and end-to-end headers are forwarded.

Hostname matching ignores case, a trailing dot, and the request port. A route matches its exact hostname or a subdomain separated by a dot. IP-address routes match exactly. The most specific matching hostname wins. Unmatched hosts return 404.

All HTTP methods are relayed. Escaped paths, query parameters, response status, repeated headers, and streaming responses are preserved. Redirects are returned without following them. Forwarded client metadata is generated from the incoming connection, and upstream failures return 502. Logging is safe under concurrent requests.


The -c or -config flag selects configuration. The -l or -log flag overrides the configured LogFile, and LOG_FILE overrides both. Access logs append to existing files.

## Build and test commands

The root Go module requires Go 1.24 or later. Dependencies are pinned by each module's go.mod and go.sum files.

build.sh builds the root module, vhoster, cameraview, and both shopping modules. test.sh runs every module's tests with race detection and then go vet. Tests use local handlers and temporary stores.

run.sh builds Authentigate, starts it in development mode, and starts the demonstration modules. modules/run.sh builds menu and presence before starting them and the shopping launcher. The Authentigate, menu/presence, and vhoster launchers stop if their own build fails and retain existing binaries until their replacements build successfully. Generated binaries are ignored by Git.
