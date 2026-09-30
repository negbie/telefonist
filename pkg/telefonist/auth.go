package telefonist

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

var sessionCookieName = "session"

var authState = struct {
	sync.Mutex
	sessions map[string]time.Time
	attempts map[string][]time.Time
}{sessions: make(map[string]time.Time), attempts: make(map[string][]time.Time)}

func validSession(token string) bool {
	authState.Lock()
	defer authState.Unlock()
	expiry, ok := authState.sessions[token]
	if !ok || time.Now().After(expiry) {
		delete(authState.sessions, token)
		return false
	}
	return true
}

func loginAllowed(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	authState.Lock()
	defer authState.Unlock()
	now := time.Now()
	for key, times := range authState.attempts {
		if len(times) == 0 || now.Sub(times[len(times)-1]) > time.Minute {
			delete(authState.attempts, key)
		}
	}
	if len(authState.attempts) > 1024 {
		return false
	}
	var recent []time.Time
	for _, at := range authState.attempts[host] {
		if now.Sub(at) < time.Minute {
			recent = append(recent, at)
		}
	}
	if len(recent) >= 10 {
		return false
	}
	authState.attempts[host] = append(recent, now)
	return true
}

func GenerateSessionToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate secure session token: %w", err)
	}
	return hex.EncodeToString(b), nil
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
		if err != nil || !validSession(cookie.Value) {
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
		if !loginAllowed(r.RemoteAddr) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "Too many login attempts", http.StatusTooManyRequests)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)

		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		if req.Username == "admin" && adminPassword != "" && subtle.ConstantTimeCompare([]byte(req.Password), []byte(adminPassword)) == 1 {
			sessionToken, err := GenerateSessionToken()
			if err != nil {
				http.Error(w, "Login unavailable", 503)
				return
			}
			authState.Lock()
			for token, expiry := range authState.sessions {
				if time.Now().After(expiry) {
					delete(authState.sessions, token)
				}
			}
			if len(authState.sessions) >= 256 {
				authState.Unlock()
				http.Error(w, "Session limit reached", 503)
				return
			}
			authState.sessions[sessionToken] = time.Now().Add(24 * time.Hour)
			authState.Unlock()
			http.SetCookie(w, &http.Cookie{
				Name:     sessionCookieName,
				Value:    sessionToken,
				Path:     "/",
				HttpOnly: true,
				Secure:   r.TLS != nil,
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
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		authState.Lock()
		delete(authState.sessions, cookie.Value)
		authState.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	w.WriteHeader(http.StatusOK)
}
