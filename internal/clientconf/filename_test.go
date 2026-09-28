package clientconf

import (
	"strings"
	"testing"
)

func TestConfigFilename(t *testing.T) {
	cases := []struct {
		name                         string
		prefix, username, id, suffix string
		want                         string
	}{
		{"plain", "", "alice", "device-one", "", "alice-awekcnaz.conf"},
		{"prefix+suffix", "wg", "alice", "device-one", "v2", "wgalv2-awekcnaz.conf"},
		{"unsafe chars", "w.g", "ali ce", "device-one", "v-2", "wgalv2-awekcnaz.conf"},
		{"long username", "", "averyveryverylongusernamexxxx", "device-one", "", "averyv-awekcnaz.conf"},
		{"second device", "", "alice", "device-two", "", "alice-f662qpfk.conf"},
		{"empty label", "", "", "device-one", "", "wg-awekcnaz.conf"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ConfigFilename(tc.prefix, tc.username, tc.id, tc.suffix)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			if len(strings.TrimSuffix(got, ".conf")) > 15 {
				t.Fatalf("profile stem too long: %q", got)
			}
		})
	}
	if got := ConfigArchiveFilename("averyveryverylongusernamexxxx"); got != "averyveryverylonguse-configs.zip" {
		t.Fatalf("archive filename = %q", got)
	}
}
