package telefonist

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLogoutRevokesOnlyThatSessionAndExpiryIsServerEnforced(t *testing.T) {
	login := func() *http.Cookie {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "https://localhost/api/login", strings.NewReader(`{"username":"admin","password":"test-password"}`))
		r.RemoteAddr = "192.0.2.55:1234"
		r.TLS = &tls.ConnectionState{}
		HandleLogin("test-password")(w, r)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		cookies := w.Result().Cookies()
		if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly {
			t.Fatal("insecure cookie")
		}
		return cookies[0]
	}
	a, b := login(), login()
	defer func() {
		authState.Lock()
		delete(authState.sessions, a.Value)
		delete(authState.sessions, b.Value)
		authState.Unlock()
	}()
	if a.Value == b.Value {
		t.Fatal("sessions share token")
	}
	req := httptest.NewRequest("POST", "/api/logout", nil)
	req.AddCookie(a)
	HandleLogout(httptest.NewRecorder(), req)
	if validSession(a.Value) || !validSession(b.Value) {
		t.Fatal("incorrect logout revocation")
	}
	authState.Lock()
	authState.sessions[b.Value] = time.Now().Add(-time.Second)
	authState.Unlock()
	if validSession(b.Value) {
		t.Fatal("expired session accepted")
	}
}
