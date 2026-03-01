package tools

import (
	"encoding/json"
	"net/http"
)

// DumpHeader is only used for debugging purposes. It dumps the header of a http.Response.
func DumpHeader(resp http.Response) {
	header, _ := json.MarshalIndent(resp.Header, "", "  ")
	println(string(header))
}
