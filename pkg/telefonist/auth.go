package telefonist

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
)

var sessionToken string
var sessionCookieName = "session"

func GenerateSessionToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate secure session token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func init() {
	var err error
	sessionToken, err = GenerateSessionToken()
	if err != nil {
		log.Printf("failed to generate session token: %v", err)
	}
}

func AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login.html" ||
			r.URL.Path == "/login.js" ||
			r.URL.Path == "/api/login" ||
			r.URL.Path == "/style.css" ||
			strings.HasPrefix(r.URL.Path, "/icons/") {
			next(w, r)
			return
		}

		cookie, err := r.Cookie(sessionCookieName)
		if err != nil || cookie.Value != sessionToken {
			if r.URL.Path == "/" || r.URL.Path == "/index.html" {
				http.Redirect(w, r, "/login.html", http.StatusFound)
				return
			}
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func HandleLogin(adminPassword string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		if req.Username == "admin" && req.Password == adminPassword {
			http.SetCookie(w, &http.Cookie{
				Name:     sessionCookieName,
				Value:    sessionToken,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
				MaxAge:   86400, // 1 day
			})
			w.WriteHeader(http.StatusOK)
			if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
				log.Printf("failed to encode login response: %v", err)
			}
			return
		}

		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
	}
}

func HandleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	w.WriteHeader(http.StatusOK)
}
