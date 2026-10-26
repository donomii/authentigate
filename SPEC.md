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

## Shopping demonstration

The shopping applications demonstrate baskets, coupons, and orders; they do not collect card details or process payments. Mutating operations use POST. Frontend forms carry signatures bound to the user and trusted bearer session. Forms remain valid across frontend replicas and restarts; missing sessions, mismatched signatures, and forms from a rotated session are rejected. Backend mutations require the application's request header. Read operations return item, basket, checkout, and coupon information without changing store state.

Coupons have a discount, target item, expiry, and usage state. Expired or used coupons do not discount another order. Listing and deleting coupons perform their named actions, and deletions are saved. Item quantities must be valid positive integers. Order calculation handles combinations without repeated subtraction for large quantities. File-backed updates are serialized and saved atomically; malformed saved data is reported rather than silently replaced with an empty shop or user.
