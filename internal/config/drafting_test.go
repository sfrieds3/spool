package config

import "testing"

func TestDraftingConfiguration(t *testing.T) {
	for _, sources := range []string{"", `,"sources":[]`} {
		c, e := Parse([]byte(`{"version":1,"drafting":{"provider":"example","model":"exact-model"}`+sources+`}`), fakeEnv("/home/example", nil))
		if e != nil || c.Drafting == nil || c.Drafting.TimeoutSeconds != 120 || c.Drafting.MaxInputBytes != 131072 {
			t.Fatalf("%#v %v", c, e)
		}
	}
	for _, d := range []string{`null`, `{}`, `{"provider":"p"}`, `{"provider":"p","model":"m","timeout_seconds":0}`, `{"provider":"p","model":"m","timeout_seconds":601}`, `{"provider":"p","model":"m","max_input_bytes":1048577}`, `{"provider":"p","model":"m","thinking":"yes"}`, `{"provider":"p","model":"m","api_key":"secret"}`, `{"provider":"p","model":"m","model":"n"}`, `{"provider":"p","model":"m","thinking":null}`} {
		if _, e := Parse([]byte(`{"version":1,"drafting":`+d+`}`), fakeEnv("/home/example", nil)); !isInvalid(e) {
			t.Errorf("accepted %s: %v", d, e)
		}
	}
}
