# authentigate
An edge server that manages user sessions via oauth2.  Great for small/personal installations.

Authentigate increases your security by protecting your app against several kinds of hacks, and handles user logins and sessions so that you don't have to.

It accepts requests, authenticates them, and passes the request to the correct (micro)service.  authentigate increases security by limiting the kind and amount of data that will be sent to (micro)services.

To increase security and GDPR compliance, authentigate does not store user data or passwords, it instead uses oauth2 to authenticate users.  Even if an authentigate server is completely compromised, no user data will be lost, not even passwords (although all session tokens would need to be revoked, forcing users to log in again).

Authentigate uses Let'sEncrypt to provide HTTPS access.


Authentigate is still in development.  Some features are not fully implemented, and there are certainly errors.

# Setup

    go get github.com/donomii/authentigate
    cd go/src/github.com/donomii/authentigate
    go build .
    vim provider_secrets.json
    vim config.json
    ./authentigate
  
# Configuration files

There are two configuration files, provider_secrets.json and config.json.  config.json provides the routes and network details for the server like port, hostname, etc.  See the included config.json for a concrete example.

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

Be aware that you will usually have to register the "redirectURL" with the oauth2 provider, and sometimes this registration is on a different page.

A sample file called provider_secrets.json.examples can be found in the repository.  Rename it to provider_secrets.json and add your details.

# Integration

Rather than relaying the entire request from client to microservice, authentigate creates a new request, and only copies what is necessary.  It also adds four HTTP headers: authentigate-id, authentigate-token, authentigate-base-url, authentigate-top-url.

Rather than relaying the entire request, authentigate creates a new request, and only copies what is necessary for the request.  It also adds four HTTP header fields: authentigate-id, authentigate-token, authentigate-base-url, authentigate-top-url.  You can use these in your program to find out which user is logged in, and how to generate links that work with authentigate.

## authentigate-id

This is authentigate's internal user id.  You should not show it to the user, and you should use it as a key for user data

## authentigate-token

This is the revocable session token that the client is currently using. You can use this to construct automatic login urls that will work with e.g. curl

## authentigate-base-url

The **external** base url of your website (with session token)

## authentigate-top-url

The **external** base url of your microservice (with session token).  You would add your API path to the end of this.

