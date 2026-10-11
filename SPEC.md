# Authentigate behavior

Authentigate authenticates users and forwards their requests to configured HTTP services. The root application owns OAuth login, authenticator login, user identity, and revocable sessions. The separate vhoster application routes HTTP requests by hostname. The menu, presence, and shopping applications demonstrate integration with the identity headers supplied by Authentigate.

## Running and checking

The root Go module requires Go 1.26. The dependency manifests and checksums define the build inputs. `build.sh` compiles every module; `test.sh` runs the regression suites with race detection and static checks, including the separately versioned applications. Tests use temporary storage and local HTTP servers and do not require OAuth credentials or external services.

`run.sh` starts the root server in development mode and starts the demonstration applications. Development mode listens on loopback port 8000 and offers a password-free development login. `vhoster/run.sh` starts vhoster in development mode on port 3000. Launchers build before starting and stop on build failures. Normal root startup reads configuration and uses automatic HTTPS when `Secure` is true; `-develop` selects development behavior. `-base-url` overrides the public authenticated URL prefix.

## Root configuration

The root application reads `config.json` from its working directory. Invalid JSON, incompatible field types, missing required routing information, and invalid limits are startup errors. The supplied configuration files include editable default limits.

| Field | Meaning and default |
| --- | --- |
| `Redirects` | Routes containing `Name`, `From`, `To`, `Tipe`, and optional `CopyHeaders`. |
| `Name` | Service name used in identity headers and diagnostics. Required for each route. |
| `From` | Absolute public route pattern. `:token` supplies a session token and `*api` captures the remaining service path. |
| `To` | Absolute HTTP or HTTPS upstream URL. Its path is the prefix for the captured API path. |
| `Tipe` | HTTP method handled by the route: `GET`, `POST`, or `PUT`. Configure separate entries for each supported method. |
| `CopyHeaders` | JSON array of additional request headers to forward. Identity and forwarding headers always come from the gateway. |
| `Port` | Loopback HTTP listening port when automatic HTTPS is disabled; an integer from 1 through 65535. |
| `BaseUrl` | Absolute public authenticated URL prefix ending in `/`, normally `https://example.com/secure/`. |
| `HostNames` | Hostnames for automatic HTTPS certificates. At least one is required when `Secure` is true. |
| `Bans` | Substrings rejected in authenticated request URLs with status 403. |
| `LogFile` | Access log path; defaults to `accessLog`. Existing log contents are preserved. |
| `Secure` | Enables automatic HTTPS; defaults to true. False selects loopback HTTP using `Port`. |
| `MaxBodyBytes` | Maximum buffered request or response body, and maximum WebSocket message size; defaults to 33554432 bytes (32 MiB). Increase it for larger uploads or responses. |
| `UpstreamTimeoutSeconds` | Maximum HTTP upstream request duration, WebSocket handshake duration, and individual WebSocket write duration; defaults to 30 seconds. |

`provider_secrets.json` maps provider names to `clientID`, `clientSecret`, and `redirectURL` strings. The callback URL must identify that provider's callback on this gateway. These credentials are loaded before normal serving starts. Development login does not require provider credentials.

## Identity and sessions

An internal user ID is a UUID string, except for the reserved development ID `1`. A session token is an opaque bearer credential. Newly generated tokens contain 32 bytes of cryptographically generated randomness encoded for use in URLs. A token identifies exactly one user and must equal that user's current saved token. Development identities cannot authenticate outside development mode.

User records contain the internal ID, provider identities, current token, authenticator secret, optional username, and authenticator replay state. Provider identities retain the existing provider-name plus provider-user-ID representation. Usernames map to internal IDs. Session mappings store internal IDs as JSON strings, not encoded byte arrays.

The file stores live under the configured database filename prefix, which defaults to `authentigate_`. The `users`, `foreignIDs`, `sessionTokens`, and `userNames` stores contain JSON files keyed by their respective IDs. Persistence failures are errors rather than successful account updates. Account mutations are serialized within one gateway process.

An authenticated request may carry `AuthentigateSessionToken` as a cookie or a session token in the route. A present cookie takes precedence. New cookies are HTTP-only, use SameSite=Lax, and are secure on HTTPS installations. Account and login pages are not cacheable. Session credentials and authenticator secrets are excluded from application diagnostic output.

Rotating a token creates a new credential, saves it as the current credential, removes the previous mapping, and updates the browser cookie. The authenticator secret survives rotation. An old token is invalid even if a stale mapping remains on disk.

## Login and account pages

The home page offers configured OAuth providers, authenticator login, and the development login when enabled. OAuth callbacks must match the browser's login cookie, provider, and unexpired signed state before an authorization code is exchanged. Provider errors and empty provider identities do not create accounts.

`GET /totplogin` displays the username and authenticator-code form without requiring a session. `POST /totplogin` verifies the submitted account and time-based code before setting the session cookie and displaying the success page. Failed and repeated attempts are bounded; an accepted time-step code cannot be reused.

`GET /manage/:token/token` displays the current credential, account form, and authenticator QR image. The QR image is embedded in the authenticated page; there is no public QR-file endpoint. `GET /manage/:token/updateUser` and `GET /manage/:token/newToken` display account controls without changing data. Their POST forms require the matching session's hidden security value before changing the username or rotating the token. Values inserted into account HTML are escaped.

Successful login displays a link to the configured menu. The shipped success page also navigates there after five seconds. A missing or invalid session displays the login page instead of forwarding a request.

## HTTP and WebSocket relay

The relay combines the upstream path prefix with the captured API path and preserves escaped path bytes and query parameters. It forwards selected request headers, removes hop-by-hop headers, and replaces identity and client-address metadata with gateway-generated values. The built-in request headers include content type, content disposition, accept, and accept language. Request content length matches the buffered body.

The gateway supplies `authentigate-id`, `authentigate-token`, `authentigate-base-url`, `authentigate-base-token-url`, and `authentigate-top-url`. They identify the authenticated user, current session, cookie-based service URL, token-based service URL, and authenticated site URL, respectively. Client-supplied versions never override these values.

HTTP upstream status codes and end-to-end response headers are preserved, including multiple cookies. Redirects are returned to the caller instead of followed with gateway credentials. Requests exceeding the body limit return 413. Upstream connection/read failures return 502; upstream timeouts return 504. Oversized upstream responses return 502. Request cancellation is propagated to the upstream operation. Response bodies are closed after use.

WebSocket connections preserve the upstream URL query, authenticated headers, and negotiated subprotocol. HTTPS upstreams use secure WebSockets. Browser origins must match the requested gateway host. The upstream handshake succeeds before the client connection is upgraded. Both directions enforce the configured message limit and close when either direction ends.

Access log entries include the client address, user identity, route pattern, method, status, and response size. They omit query strings and credential-bearing referrer paths and are written without an unflushed shared buffer.

## Hostname routing

Vhoster reads its own `config.json`. Each redirect requires `Host` and an absolute HTTP or HTTPS `To`; `Name` is descriptive. `HostNames` selects certificate names, and `LogFile` defaults to `accessLog`. `Port` and `CopyHeaders` remain accepted compatibility fields; development listens on port 3000 and end-to-end headers are forwarded.

Hostname matching ignores case, a trailing dot, and the request port. A route matches its exact hostname or a subdomain separated by a dot. IP-address routes match exactly. The most specific matching hostname wins. Unmatched hosts return 404.

All HTTP methods are relayed. Escaped paths, query parameters, response status, repeated headers, and streaming responses are preserved. Redirects are returned without following them. Forwarded client metadata is generated from the incoming connection, and upstream failures return 502. Logging is safe under concurrent requests.

## Demonstration applications

The menu uses the gateway identity for stored messages and images. An account with no stored message receives an empty value; a one-byte message is retained. Multipart uploads reject missing/invalid image data and excessive sizes. Images are served with their validated media type and browser content sniffing disabled. Account links lead to the gateway's account controls.

Presence rooms store named participants, last update time, external address, and local addresses. Polling retains the room ID. Participant names and addresses are escaped when rendered, and values embedded in script are serialized as JSON. The presence client parses interface addresses with network prefixes, encodes query parameters, bounds request duration, and avoids printing credential URLs.

The shopping applications demonstrate baskets, coupons, and orders; they do not collect card details or process payments. Mutating operations use POST. Frontend forms carry signed values bound to the user, and backend mutations require the application's request header. Read operations return item, basket, checkout, and coupon information without changing store state.

Coupons have a discount, target item, expiry, and usage state. Expired or used coupons do not discount another order. Listing and deleting coupons perform their named actions, and deletions are saved. Item quantities must be valid positive integers. Order calculation handles combinations without repeated subtraction for large quantities. File-backed updates are serialized and saved atomically; malformed saved data is reported rather than silently replaced with an empty shop or user.
