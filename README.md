# Go-Time-Recording

Go-Time-Recording is tool to record time, built with the gofr framework:

- Build up database (Postgres / MongoDB) with migrations
- Start an echo server with env files (.env.local / env.secrets.local)
- Configure the acl based on rbac and get access via basic auth
- Use helper functions for parsing date / time, nested xml to struct or nullsql datatypes
- Use the env handler to load env values for your config
- Use the http handler to send an request and handle the response via REST
- Use the error handler as middleware in echo with predefined generic errors which mapped to status codes
- Use extended logging (debug / info / warn / error) also the provided middlewares in echo to log request or dump the body
- Use the recover handler as middleware in echo to recover by panic
- Use the secure handler as middleware in echo to provide content security policy and security headers
- Use the test handler to create a cotnext with a valid value for testing
- Use tracing (opentelemetry) for monitoring with tools like jaeger
- Use the util functions to create a tls config, increase retries, stringify a map or create a uuid
- Use the validation as middleware in echo to validate via extended tags (depends_on / depends_one_of)

## Install

```bash
go get github.com/dennis-dko/go-time-recording
```

## Usage

Parse nested xml to struct

```go
package main

import (
	"fmt"

	"github.com/dennis-dko/go-toolkit/datatype"
)

type ParseStruct struct {
	XMLName    string `json:"-" nxml:"//users/user"`
	FirstName  string `nxml:"//user/@firstName" json:"first_name"`
	LastName   string `nxml:"//user/@lastName" json:"last_name"`
	Age        uint8  `nxml:"//user/@age" json:"age"`
	Gender     string `nxml:"//user/@gender" json:"gender"`
	Company    string `nxml:"//user/@company" json:"company"`
	Email      string `nxml:"//user/postal/street/email" json:"email"`
	Pet        string `nxml:"//user/postal/street/animal/@pet" json:"pet"`
	Street     string `nxml:"//user/postal/street/@address" json:"street"`
	PostalCode int    `nxml:"//user/postal/@code" json:"postal_code"`
}

func main() {
    var parseStructList []ParseStruct

    xml := []byte(`<users count="2"><user firstName="Walter" lastName="White" age="50" gender="male" company="T"><postal code="91764"><street address="Villa Gaeta"><email>walter.white@example.com</email><animal pet="cat"></animal></street></postal></user><user firstName="James" lastName="McGill" age="45" gender="male" company="S"><postal code="65782"><street address="Saul Street"><email>james.mcgill@example.com</email><animal pet="dog"></animal></street></postal></user></users>`)

    err := datatype.ParseXMLToStruct(string(xml), &parseStructList)
    if err != nil {
        panic(err)
    }

    fmt.Printf("%+v\n", parseStructList)
}
```

For more details check out the test files or the _example directory.

## License

MIT license.

## Info

### Create migrations like this:

https://github.com/golang-migrate/migrate/tree/master/database/postgres

##### Note: only available for postgres