package clientconf

import "testing"

func TestConfigFilename(t *testing.T) {
	cases := []struct {
		name                           string
		prefix, username, device, suff string
		want                           string
	}{
		{"plain", "", "alice", "phone", "", "alice-phone.conf"},
		{"prefix+suffix", "wg", "alice", "phone", "v2", "wg-alice-phone-v2.conf"},
		{"unsafe chars", "", "ali ce", "my/phone", "", "ali-ce-my-phone.conf"},
		{"unicode dropped", "", "user1", "دستگاه", "", "user1.conf"},
		{"long kept under cap", "", "averyveryverylongusernamexxxx", "d", "", "averyveryverylongusernamexxxx-d.conf"},
		{"all empty", "", "", "", "", "config.conf"},
		{"dots kept", "v.1", "a.b", "c.d", "e.9", "v.1-a.b-c.d-e.9.conf"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ConfigFilename(tc.prefix, tc.username, tc.device, tc.suff)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
