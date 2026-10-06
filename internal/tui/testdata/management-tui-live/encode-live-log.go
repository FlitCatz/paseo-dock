// Preserve byte-exact PTY output with CR/escape characters in a reviewable JSON string.
package main

import (
	"encoding/json"
	"os"
)

func main() {
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	out, err := json.Marshal(string(data))
	if err != nil {
		panic(err)
	}
	if err = os.WriteFile(os.Args[2], append(out, '\n'), 0600); err != nil {
		panic(err)
	}
}
