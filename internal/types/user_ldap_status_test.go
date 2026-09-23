package types

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUserInfoReportsLDAPBindingWithoutExposingDirectoryDN(t *testing.T) {
	subject := "uid=alice,ou=people,dc=example,dc=com"
	user := &User{ID: "user-1", LDAPSubject: &subject}
	view := user.ToUserInfo()
	if !view.LDAPBound {
		t.Fatal("bound account was not reported as bound")
	}
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), subject) || !strings.Contains(string(encoded), `"ldap_bound":true`) {
		t.Fatalf("user response leaked LDAP subject or omitted status: %s", encoded)
	}
	if (&User{}).ToUserInfo().LDAPBound {
		t.Fatal("unbound account was reported as bound")
	}
}
