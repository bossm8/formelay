package email

import "testing"

func TestReplyToField(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want string
	}{
		{"unset", Config{}, ""},
		{"set", Config{ReplyToField: "message"}, "message"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n := &emailNotifier{cfg: c.cfg}
			if got := n.ReplyToField(); got != c.want {
				t.Fatalf("ReplyToField() = %q, want %q", got, c.want)
			}
		})
	}
}
