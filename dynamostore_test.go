package dynamostore

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/sessions"
	"github.com/icrowley/fake"
)

func TestNewDynamoStore(t *testing.T) {

	// Create Store 1
	_, err := NewDynamoStore(map[string]interface{}{
		"table":         fake.CharactersN(10),
		"region":        "",
		"read_capacity": -1,
		"endpoint":      "http://localhost:8000",
		"ttl_enabled":   false,
	}, []byte("secret-key"))
	if err != nil {
		t.Errorf("expected nil; got %v", err)
		return
	}
}

func TestSessionLifecycle(t *testing.T) {

	var req *http.Request
	var res *httptest.ResponseRecorder
	var session *sessions.Session
	var err error

	// Create Store 1
	store, err := NewDynamoStore(map[string]interface{}{
		"region":      "ap-south-1",
		"endpoint":    "http://localhost:8000",
		"ttl_enabled": true,
	}, []byte("sessionSecret"))
	if err != nil {
		t.Errorf("expected nil; got %v", err)
		return
	}

	store.MaxAge(86400 * 30 * 30)

	req, _ = http.NewRequest("GET", "http://localhost:8000/", nil)
	res = httptest.NewRecorder()

	// Testing New session.
	if session, err = store.New(req, "mysession"); err != nil {
		t.Errorf("expected nil; got %v", err)
		return
	}

	if !session.IsNew {
		t.Error("expected new session, got existing session")
		return
	}

	// Testing Save session
	session.Values["name"] = "alice"
	session.Values["id"] = 43

	if err = session.Save(req, res); err != nil {
		t.Errorf("expected nil; got %v", err)
		return
	}

	// Testing existing session

	req.AddCookie(res.Result().Cookies()[0])
	existingSession, err := store.Get(req, "mysession")
	if err != nil {
		t.Errorf("expected nil; got %v", err)
		return
	}

	if existingSession.IsNew {
		t.Error("expected existing session, got new session")
		return
	}

	if existingSession.Values["name"] != "alice" {
		t.Error("session values didn't match")
		return
	}

	// Testing Delete session
	existingSession.Options.MaxAge = -1
	if err = existingSession.Save(req, res); err != nil {
		t.Errorf("expected nil; got %v", err)
		return
	}

}
