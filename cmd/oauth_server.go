package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
)

func mockOAuthServer() *httptest.Server {
	mux := http.NewServeMux()

	// Mock authorization endpoint
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		redirectUrl := r.FormValue("redirect_uri")
		state := r.FormValue("state")
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body>
<form action="/authorize-user" method="post">
  <label for="email">Authenticate as:</label>
  <select name="email" id="email">
    <option value="test@example.com">test@example.com</option>
    <option value="another@example.com">another@example.com</option>
  </select>
  <input type="hidden" name="redirect_uri" value="` + redirectUrl + `">
  <input type="hidden" name="state" value="` + state + `">
  <button type="submit">Login</button>
</form>
</body></html>`))

	})

	mux.HandleFunc("/authorize-user", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.FormValue("redirect_uri")+"?code=mock-code:"+r.FormValue("email")+"&state="+r.FormValue("state"), http.StatusFound)
	})

	// Mock token endpoint
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		code := r.FormValue("code")
		if strings.Contains(code, "test@example.com") {
			_, _ = w.Write([]byte(`{
			"access_token": "mock-access-token:test@example.com",
			"token_type": "bearer",
			"expires_in": 3600,
			"scope": "user:email"
		}`))
		} else if strings.Contains(code, "another@example.com") {
			_, _ = w.Write([]byte(`{
			"access_token": "mock-access-token:another@example.com",
			"token_type": "bearer",
			"expires_in": 3600,
			"scope": "user:email"
		}`))
		} else {
			http.Error(w, "Invalid code", http.StatusBadRequest)
		}
	})

	// Mock user emails endpoint
	mux.HandleFunc("/user/emails", func(w http.ResponseWriter, r *http.Request) {
		fmt.Println(r.Header.Get("Authorization"))
		if strings.Contains(r.Header.Get("Authorization"), "test@example.com") {
			w.Header().Set("Content-Type", "application/json")
			emails := []Email{
				{
					Email:    "test@example.com",
					Primary:  true,
					Verified: true,
				},
			}
			err := json.NewEncoder(w).Encode(emails)
			if err != nil {
				fmt.Println(err)
			}
		} else if strings.Contains(r.Header.Get("Authorization"), "another@example.com") {
			w.Header().Set("Content-Type", "application/json")
			emails := []Email{
				{
					Email:    "another@example.com",
					Primary:  true,
					Verified: true,
				},
			}
			err := json.NewEncoder(w).Encode(emails)
			if err != nil {
				fmt.Println(err)
			}
		} else {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
		}
	})

	server := httptest.NewUnstartedServer(mux)
	listener, _ := net.Listen("tcp", "localhost:9001")
	server.Listener = listener
	server.Start()
	return server
}
