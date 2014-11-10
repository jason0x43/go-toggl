go-toggl
========

A Go library for accessing the Toggl API

Usage
-----

Include the dependency `github.com/jason0x43/go-toggl` in your code. The
library uses the "toggl" namespace by default. 

The repository includes a simple test project. Build it with:

    `go build github.com/jason0x43/go-toggl/toggl`

Run it with your Toggl API token as:

    `./toggl abc123`

Assuming everything’s working properly, the program will download your account
information from Toggl and dump it to the console as a JSON object.
