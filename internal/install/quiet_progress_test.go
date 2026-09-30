package install

import (
	"strings"
	"testing"
)

func TestQuietCommandLabelsDescribeWorkWithoutLeakingArguments(t *testing.T) {
	for _, tc := range []struct {
		argv []string
		want string
	}{
		{[]string{"apt-get", "update"}, "Refreshing Ubuntu package index"},
		{[]string{"apt-get", "-o", "DPkg::Lock::Timeout=300", "install", "secret-package"}, "Installing required Ubuntu packages"},
		{[]string{"docker", "build", "--label", "token=secret"}, "Building verified Docker runtime"},
		{[]string{"dkms", "install", "-v", "secret-version"}, "Building AmneziaWG kernel module"},
		{[]string{"nginx", "-t"}, "Checking Nginx configuration"},
		{[]string{"/usr/local/bin/wg-guard", "settings", "set", "token", "secret"}, "Applying initial panel settings"},
	} {
		got := quietCommandLabel(tc.argv)
		if got != tc.want || strings.Contains(got, "secret") {
			t.Fatalf("argv %v produced label %q; want %q", tc.argv, got, tc.want)
		}
	}
}
