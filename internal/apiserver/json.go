package apiserver

import (
	"encoding/json"
	"net/http"
)

// jsonEncode encodes data to JSON and writes it to the response writer
func jsonEncode(w http.ResponseWriter, data interface{}) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}
