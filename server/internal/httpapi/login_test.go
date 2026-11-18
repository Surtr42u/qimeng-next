package httpapi_test

import (
	"bytes"
	"testing"

	"qimeng.local/next/server/internal/httpapi"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
)

func TestPasswordCreatesIndependentSessionsAndCannotAuthenticateAPI(t *testing.T) {
	const password = "synthetic-login-password-only"
	s, e := service.New(service.Config{DataDir: t.TempDir(), AdminToken: testSecret, AdminPassword: password, ReserveBytes: 1})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	f := &fixture{t: t, s: s, h: httpapi.New(s)}
	if s.Config.AdminPassword != "" {
		t.Fatal("plaintext password retained in runtime configuration")
	}
	r := f.request("GET", "/sessions/current", "", password, nil, nil)
	if r.Code != 401 {
		t.Fatal("password was accepted as an API token")
	}
	sessions := []model.Principal{}
	for _, label := range []string{"device one", "device two"} {
		r = f.json("POST", "/sessions", "", map[string]string{"password": password, "label": label}, 201)
		var p model.Principal
		decode(t, r, &p)
		if p.Token == password || p.Token == testSecret || p.Token == "" || bytes.Contains(r.Body.Bytes(), []byte(password)) {
			t.Fatal("login exposed source credential")
		}
		sessions = append(sessions, p)
	}
	if sessions[0].ID == sessions[1].ID || sessions[0].Token == sessions[1].Token {
		t.Fatal("device sessions were not independent")
	}
	r = f.request("DELETE", "/sessions/current", "", sessions[0].Token, nil, nil)
	if r.Code != 204 {
		t.Fatal(r.Body.String())
	}
	if f.request("GET", "/sessions/current", "", sessions[0].Token, nil, nil).Code != 401 {
		t.Fatal("logged-out session still active")
	}
	if f.request("GET", "/sessions/current", "", sessions[1].Token, nil, nil).Code != 200 {
		t.Fatal("one logout revoked a different device")
	}
	f.json("POST", "/sessions", "", map[string]string{"password": "wrong password", "label": "wrong"}, 401)
	f.json("POST", "/sessions", "", map[string]string{"password": password, "token": testSecret, "label": "ambiguous"}, 400)
	f.json("POST", "/sessions", "", map[string]string{"label": "empty"}, 400)
}
