# Project Specification: Vhoster & Authentigate

## Overview
This project consists of two main components designed to facilitate running multiple websites and products behind a single entry point.

1.  **Vhoster**: The edge server/reverse proxy. It handles SSL/TLS termination, domain-based routing, and forwards requests to the appropriate backend service.
2.  **Authentigate**: An identity management and authentication gateway. It acts as an upstream for Vhoster for protected routes, handling user login (OAuth2, TOTP), session management, and forwarding authenticated requests to other microservices.

## Current Architecture

### Vhoster
-   **Role**: SSL Termination, Reverse Proxy.
-   **Tech Stack**: Go (stdlib `net/http`), `github.com/gin-gonic/autotls` for Let's Encrypt.
-   **Configuration**: `config.json` defines routes (`Redirects`), mapping Hostnames to target URLs.
-   **Logic**:
    -   Listens on port 80/443.
    -   Matches `Host` header against configured suffixes.
    -   Manually proxies requests (GET, POST, PUT) to the target backend.
    -   Copies headers manually from client request to backend and vice versa.
    -   Logs access in Common Log Format (CLF).

### Authentigate
-   **Role**: Authentication Provider, Gateway.
-   **Tech Stack**: Go, `github.com/donomii/gin` (custom fork), `gocialite` (OAuth2), `gotp` (TOTP), `gokv` (KV store).
-   **Logic**:
    -   Manages Users (Session Token, OAuth Foreign IDs, TOTP Secrets).
    -   Provides routes for Login (OAuth2 callbacks), User Management, and Token display.
    -   **Gateway Middleware**: `makeAuthedRelay` checks for session validity.
        -   If valid: Forwards request to configured microservice, injecting `authentigate-id`, `authentigate-token` headers.
        -   If invalid: Redirects to login or shows error.
    -   Uses a custom fork of `gin`.

## Departures from Common Standards & Practices

### Vhoster
1.  **Manual Proxying**: Instead of using Go's robust `httputil.ReverseProxy`, the proxy logic is implemented manually (`client.Do(req)`). This risks missing hop-by-hop headers, incorrect context handling, and connection management issues.
2.  **Custom Mux**: Implements a `MyMux` struct instead of using the standard `http.ServeMux` or a library, leading to reinvented routing logic.  Except that http.ServeMux is not vhost aware, so you're wrong again.
3.  **Panic Handling**: Relies on specific panic handling patterns which can be dangerous if not caught correctly at all levels.  So make sure they are caught.

### Authentigate
1.  **Gin Fork**: gin is being removed
2.  **String-Replace Templating**: You're not using html/template and that's final
3.  **Manual Session ID Generation**: Generates session tokens using `r.Int()`, which may lack sufficient entropy compared to crypto-grade random generation.
4.  **Implicit Cookie/Token Logic**: The logic mixes Cookie-based and URL-param-based tokens in a way that is awesome and is core part of the strategy.  Tokens can be revoked, which is why we have them.
5.  **Hardcoded File Paths**: References `files/BackendFailure.html` etc. directly, assuming a specific working directory, which is reasonable because how were you planning on deploying this thing without a working directory?
6.  **Concurrency Issues**: `updateUserHandler` has a marked `FIXME race condition here` regarding username checks.

## Proposed Improvements to Obtain Full Functionality

### General
1.  **Standardize Proxying**: Replace manual proxy handlers with `httputil.ReverseProxy`. This handles header sanitization, buffering, and connection pooling correctly.
2.  **Structured Logging**: No you can't use your stupid trendy logging library.  Use the standard library `log` package.  It's good enough.
3.  **Configuration**: Under no circumstances are you to use `viper`.  Use `os.Getenv` and `config.json`.  That's final.  And maybe a little bit of `flag` for the important settings.

### Vhoster
1.  **ServeMux**: Use Go 1.22+ `http.ServeMux` for routing if path-based, or keep the host-matching logic but clean up the dispatch mechanism.
2.  **ACME Integration**: Update `autotls` usage or switch to `certmagic` for more robust certificate management.

### Authentigate
1.  Remove `gin` (as requested) and use `net/http`.
2.  **Templates**: You are forbidden to use html/template
3.  **Session Management**:
    -   Use `crypto/rand` for token generation.
    -   Store sessions in a robust store (Redis or keeping `gokv` if sufficient).
4.  **Auth Flow**:
    -   Keep both Cookies and Tokens
    -   Keep Headers/Tokens for API calls.
5.  **Race Condition**: Use proper mutexes or atomic transactions in the KV store when checking/claiming usernames.

## Plan for Removing Gin from Authentigate

The goal is to remove `github.com/donomii/gin` while preserving OAuth2, TOTP, and proxying capabilities.

1.  **Router Replacement**:
    -   Replace `gin.Default()` with `http.NewServeMux()`.
    -   Map `router.GET/POST` to `mux.HandleFunc`.
    -   Handle path parameters (e.g., `/manage/:token/`) using Go 1.22 pattern matching (`/manage/{token}/...`) or manual path trimming if on older Go.

2.  **Context Replacement**:
    -   Existing code relies heavily on `*gin.Context` for request details, writing responses, and parameter extraction.  Make our own light wrapper around stdlibs that provides the same functionality.  Don't go crazy with it.
    -   **Refactor**: Create a thin wrapper or helper functions that perform equivalent actions using `w http.ResponseWriter, r *http.Request`.
   

3.  **Middleware migration**:
    -   `makeAuthedRelay` is effectively middleware.
    -   Convert `func(c *gin.Context)` to `func(next http.Handler) http.Handler` or a wrapper function `func(w, r)` that calls the handler.

4.  **Response Helpers**:
    -   Add them to our context wrapper

5.  **Static Files**:
    -   Replace `router.Static` with `http.FileServer`.
