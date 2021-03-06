# dynamostore

[![GoDoc](https://godoc.org/github.com/pratikju/dynamostore?status.svg)](https://godoc.org/github.com/pratikju/dynamostore) [![Build Status](https://travis-ci.org/pratikju/dynamostore.svg?branch=master)](https://travis-ci.org/pratikju/dynamostore)

AWS dynamoDB store for gorilla sessions. Uses [AWS Official Go Library](https://github.com/aws/aws-sdk-go)


# Installation

```
go get -u github.com/pratikju/dynamostore
```

# Example

```go
import (
	"github.com/pratikju/dynamostore"
)

// create dynamoDB store
store, err := dynamostore.NewDynamoStore(map[string]string{
	"table":    "mysession",
	"endpoint": "http://localhost:8000", // No need to set this in production
}, []byte("something-very-secret"))
if err != nil {
  // handle error
}

// Get a session.
// Get() always returns a session, even if empty.
session, err := store.Get(r, "session-name")
if err != nil {
  // handle error
}

// Set some session values.
session.Values["name"] = "alice"
session.Values["id"] = 43

// Save the session.
if err := session.Save(r, w); err != nil {
  // handle error
}

// Delete the session
session.Options.MaxAge = -1
if err := session.Save(r, w); err != nil {
  // handle error
}
```

# License

MIT, see the [LICENSE](https://github.com/pratikju/dynamostore/blob/master/LICENSE).
