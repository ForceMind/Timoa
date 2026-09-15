package auth

import "testing"

func TestHashAndVerify(t *testing.T) {
	h, err := HashPassword("correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword("correct-horse-battery", h) {
		t.Fatal("correct password rejected")
	}
	if VerifyPassword("wrong-password", h) {
		t.Fatal("wrong password accepted")
	}
	if VerifyPassword("anything", "not-a-phc-string") {
		t.Fatal("malformed hash accepted")
	}
}

func TestShortPasswordRejected(t *testing.T) {
	if _, err := HashPassword("short"); err == nil {
		t.Fatal("short password accepted")
	}
}

func TestLoginLimiter(t *testing.T) {
	l := NewLoginLimiter()
	key := "user|127.0.0.1"
	for i := 0; i < MaxFailures; i++ {
		if !l.Allow(key) {
			t.Fatalf("attempt %d should be allowed", i)
		}
		l.Fail(key)
	}
	if l.Allow(key) {
		t.Fatal("should be locked out after MaxFailures")
	}
	l.Success(key)
	if !l.Allow(key) {
		t.Fatal("success should clear lockout")
	}
}
