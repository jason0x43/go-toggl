/*

The toggl command will display a user's Toggl account information.

Usage:
    toggl API_TOKEN

The API token can be retrieved from a user's account information page at toggl.com.

*/
package main

import (
	"encoding/json"
	"os"

	"github.com/jason0x43/go-toggl"
)

func main() {
	if len(os.Args) != 2 {
		println("usage:", os.Args[0], "API_TOKEN")
		return
	}

	session := toggl.OpenSession(os.Args[1])

	account, err := session.GetAccount()
	if err != nil {
		println("error:", err)
		return
	}

	data, err := json.MarshalIndent(&account, "", "    ")
	println("account:", string(data))
}
