package telefonist

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
)

func limitRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 4*1024*1024)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func APIKeyOrSession(key string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if key != "" && subtle.ConstantTimeCompare([]byte(key), []byte(r.Header.Get("X-API-Key"))) == 1 {
			next(w, r)
			return
		}
		AuthMiddleware(next)(w, r)
	}
}

func ensureCredentials(f *AppFlags) error {
	if f.UIAdminPassword != "" && f.UIAPIKey != "" {
		return nil
	}
	path := filepath.Join(f.DataDir, "credentials.json")
	var saved struct {
		Password string `json:"admin_password"`
		APIKey   string `json:"api_key"`
	}
	data, err := os.ReadFile(path)
	if err == nil {
		if err = json.Unmarshal(data, &saved); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if saved.Password == "" {
		saved.Password, err = GenerateSessionToken()
		if err != nil {
			return err
		}
	}
	if saved.APIKey == "" {
		saved.APIKey, err = GenerateSessionToken()
		if err != nil {
			return err
		}
	}
	data, err = json.MarshalIndent(saved, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		return err
	}
	if err = os.Chmod(path, 0600); err != nil {
		return err
	}
	if f.UIAdminPassword == "" {
		f.UIAdminPassword = saved.Password
	}
	if f.UIAPIKey == "" {
		f.UIAPIKey = saved.APIKey
	}
	return nil
}
