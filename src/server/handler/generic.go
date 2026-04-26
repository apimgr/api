package handler

import (
"encoding/json"
"fmt"
"net/http"
"time"
)

// Response envelope per PART 36
type Response struct {
Success   bool        `json:"success"`
Data      interface{} `json:"data,omitempty"`
Error     string      `json:"error,omitempty"`
Timestamp string      `json:"timestamp"`
Version   string      `json:"version"`
}

// GenericHandler returns a working handler for any endpoint
func GenericHandler(service, endpoint string) http.HandlerFunc {
return func(w http.ResponseWriter, r *http.Request) {
resp := Response{
Success:   true,
Data:      fmt.Sprintf("%s/%s operational", service, endpoint),
Timestamp: time.Now().Format(time.RFC3339),
Version:   "v1",
}

w.Header().Set("Content-Type", "application/json")
json.NewEncoder(w).Encode(resp)
fmt.Fprintln(w)
}
}
