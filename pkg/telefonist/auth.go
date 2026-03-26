package telefonist

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

var sessionToken string
var sessionCookieName = "session"

func SetSessionCookieName(name string) {
	sessionCookieName = name
}

func GenerateSessionToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		log.Fatalf("failed to generate secure session token: %v", err)
	}
	return hex.EncodeToString(b)
}

func init() {
	sessionToken = GenerateSessionToken()
}

func AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Allow login page and its dependencies
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
				MaxAge:   86400 * 1, // 1 day
			})
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
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
