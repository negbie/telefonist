package telefonist

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccountsAPIProtectsAndPreservesCredentials(t *testing.T) {
	store, err := OpenTestStore(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	handler := HandleAPISIPAccounts(&WsHub{testStore: store})
	request := func(method, body string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest(method, "/api/accounts", strings.NewReader(body)))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: HTTP %d: %s", method, w.Code, w.Body.String())
		}
		return w
	}
	request(http.MethodPost, `{"name":"alice","sip_uri":"sip:alice@example.test","password":"private-secret","uri_params":";transport=tls","addr_params":";auth_pass=parameter-secret;regint=0"}`)
	w := request(http.MethodGet, "")
	for _, secret := range []string{"private-secret", "parameter-secret"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("account listing disclosed a credential")
		}
	}
	var response struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Items) != 1 || response.Items[0]["has_password"] != true {
		t.Fatalf("missing account/password-presence information: %s", w.Body.String())
	}
	if _, exists := response.Items[0]["password"]; exists {
		t.Fatal("password field must not be serialized in account listing")
	}
	request(http.MethodPost, `{"old_name":"alice","name":"alice","sip_uri":"sip:alice@new.example.test","password":"","uri_params":";transport=tcp"}`)
	account, err := store.GetSIPAccount(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if account.Password != "private-secret" || account.SIPURI != "sip:alice@new.example.test" {
		t.Fatal("editing account metadata must preserve an omitted password")
	}
}

func TestAccountMutationRequiresAuthentication(t *testing.T) {
	store, err := OpenTestStore(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	handler := AuthMiddleware(HandleAPISIPAccounts(&WsHub{testStore: store}))
	w := httptest.NewRecorder()
	handler(w, httptest.NewRequest(http.MethodPost, "/api/accounts", strings.NewReader(`{"name":"intruder","sip_uri":"sip:x@example.test"}`)))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated mutation returned HTTP %d", w.Code)
	}
	accounts, err := store.ListSIPAccounts(context.Background())
	if err != nil || len(accounts) != 0 {
		t.Fatalf("unauthenticated request changed storage: count=%d, err=%v", len(accounts), err)
	}
}
