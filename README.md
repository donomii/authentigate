# authentigate
An edge server that manages logins and relays connections.  Great for small/personal installations that want to survive on the web.

Authentigate increases your security by protecting your web apps against several kinds of hacks, and handles user logins and sessions so that you don't have to.

It accepts requests, authenticates them, and passes the request to the correct (micro)service.  Authentigate increases security by limiting the kind and amount of data that will be sent to (micro)services.

To increase security and GDPR compliance, authentigate does not store user data or passwords, it instead uses oauth2 and TOTP to authenticate users.  Even if an authentigate server is completely compromised, no user data will be lost, not even passwords (although all session tokens would need to be revoked, forcing users to log in again).

Authentigate uses Let'sEncrypt to provide HTTPS access.

Authentigate is still in development.  Some features are not fully implemented, and there are certainly errors.

# Setup

    git clone github.com/donomii/authentigate
    cd authentigate
    go build .
    cp provider_secrets.json.example provider_secrets.json
    vim provider_secrets.json
    cp example_config.json config.json
    vim config.json
    ./authentigate
  
# Configuration files

There are two configuration files, provider_secrets.json and config.json.  config.json holds the routes to your internal services.  See the included config.json for a concrete example.

The provider_secrets.json file holds the tokens and ids needed to access online oauth2 services.

# provider_secrets

The provider_secrets file contains the secret details needed to authenticate against some popular oauth2 servers.   To get these details, you will need to register your application with these web sites.  Unfortunately each service has a different sign up process, so I can't describe them all here.  Usually, googling e.g. "github oauth2" will get you to the right place.

Once you have your details, add them to the file

```json
{
  "amazon": {
    "clientID": "xxxxxxxxxxxxxx",
    "clientSecret": "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
    "redirectURL": "http://localhost/auth/amazon/callback"
 },
 "bitbucket": {
  "clientID": "xxxxxxxxxxxxxx",
  "clientSecret": "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
  "redirectURL": "http://localhost/auth/bitbucket/callback"
  },
}
```
You must configure the redirectURL to match your servername and port.

Be aware that you will usually have to register the "redirectURL" with the oauth2 provider, and sometimes this registration is on a different page than the page where you get your clientID and clientSecret.

A sample file called provider_secrets.json.examples can be found in the repository.  Rename it to provider_secrets.json and add your details.

# Development and testing

Start authentigate with the --develop flag.  This will disable https, and all logins will be redirected to user "1".

# Integration

The entire point of authentigate is to ingtegrate with other services.  Authentigate is a full HTTP/HTTPS server, which works as a reverse proxy, relay server, or "edge" server.  It accepts requests, authenticates them, and passes the request to the correct (micro)service.  Authentigate increases security by limiting the kind and amount of data that will be sent to (micro)services.

Rather than relaying the entire request from client to microservice, authentigate creates a new request, and only copies what is necessary for the request.  This prevents clients from sending possibly dangerous additional information in the requests.  Authentigate also prevents clients from overloading your microservices by crashing before they do.

To integrate with other services, edit config.json and add the routes to your services.  See the included config.json for a concrete example.  Typically you would start your services bound to "localhost", and authentigate would relay external requests to them.

Integrating with existing web apps can be easy or difficult, depending entirely on the app.  If the app uses relative links, then it should work out of the box.  If the app uses absolute links, then you will need to edit the app to use relative links, or construct the correct external links.  Because there is no standard for this sort of thing, it is impossible for authentigate to take over authentication for all web apps.  You may have to login twice to access e.g. your email app if you put it behind authentigate.

# Header fields

Authentigate also adds four HTTP header fields: authentigate-id, authentigate-token, authentigate-base-url, authentigate-top-url.  You can use these in your program to find out which user is logged in, and how to generate links that work with authentigate.

## authentigate-id

This is authentigate's internal user id.  You should not show it to the user, and you should use it as a unique user id.  It never changes.

## authentigate-token

This is the revocable session token that the client is currently using.

## authentigate-base-url

The **external** base url of your website (with session token).  Used mainly to allow you to create links to other microservices.  You can use this to construct automatic login urls that will work with e.g. curl.  You should show this to the user, you should never use this as a key for user data.

## authentigate-top-url

The **external** base url of your microservice (with session token).  You add your API path to the end of this.  You can use this to construct automatic login urls that will work with e.g. curl.  You should show this to the user, you should never use this as a key for user data.

## Customising the login page

You can customise the login page by editing files/frontpage.html.  You can use the following variables in your template:

* BASE - the base url of your website (without session token).  Use this to return to the login page.
* TOKEN - the session token
* SECUREURL - the base url of your website (with session token).  Use this to link to other microservices.

The login page redirects the user to a default menu page.  You can choose the default menu page by editing files/loginSuccessful.html

## Special URLs

Authentigate has a few special URLs.

* BASE/manage/:token/token - shows the current user's token.  
* BASE/manage/:token/updateUser - allows the user to update their details, and activate their TOTP token.
* BASE/manage/:token/newToken - allows the user to generate a new session token.
