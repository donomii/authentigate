# authentigate
An edge server that manages user sessions via oauth2.  It accepts requests, authenticates them, and passes the request to the correct microservice.  authentigate increases security by limiting the kind and amount of data that will be sent to microservices.

To increase security and GDPR compliance, authentigate does not store user data, it instead uses oauth2 to authenticate users.  Even if an authentigate server is completely compromised, no user data will be lost, not even passwords (although all session tokens would need to be revoked, forcing users to log in again).

# Setup

  go get github.com/donomii/authentigate
  cd go/src/github.com/donomii/authentigate
  go build .
  vim provider_secrets.json
  ./authentigate
  
# Configuration

The provider_secrets file contains the secret details needed to authenticate against some popular oauth2 servers.   To get these details, you will need to register your application with these web sites.  Unfortunately each service has a different sign up process, so I can't describe them all here.  Usually, googling e.g. "github oauth2" will get you to the right place.

Once you have your details, add them to the file

```json
{                                                                                                                               "amazon": {                                                                                                                     "clientID": "xxxxxxxxxxxxxx",                                                                                           "clientSecret": "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",                                                           "redirectURL": "http://localhost:9090/auth/amazon/callback"                                                     },                                                                                                                      "bitbucket": {                                                                                                                  "clientID": "xxxxxxxxxxxxxx",                                                                                           "clientSecret": "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",                                                           "redirectURL": "http://localhost:9090/auth/bitbucket/callback"                                                    },                                                                                                                      "facebook": {   
}
```

