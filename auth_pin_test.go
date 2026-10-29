package main

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCheckPINSharesLockoutWithLogin(t *testing.T) {
	a := NewAuth("1234", newDB(t))
	clock := time.Now()
	a.now = func() time.Time { return clock }
	if ok, locked := a.CheckPIN("1234"); !ok || locked {
		t.Fatalf("PIN đúng: được (%v, %v)", ok, locked)
	}
	for i := 0; i < 5; i++ {
		if ok, _ := a.CheckPIN("0000"); ok {
			t.Fatal("PIN sai không được qua")
		}
	}
	if ok, locked := a.CheckPIN("1234"); ok || !locked {
		t.Fatalf("đang khoá: được (%v, %v)", ok, locked)
	}
	w := httptest.NewRecorder()
	a.Login(w, httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"pin":"1234"}`)))
	if w.Code != 429 {
		t.Fatalf("login đang khoá phải 429, được %d", w.Code)
	}
	clock = clock.Add(61 * time.Second)
	if ok, locked := a.CheckPIN("1234"); !ok || locked {
		t.Fatalf("hết khoá: được (%v, %v)", ok, locked)
	}
	if ok, locked := NewAuth("", newDB(t)).CheckPIN(""); ok || locked {
		t.Fatalf("PIN rỗng: được (%v, %v)", ok, locked)
	}
}
