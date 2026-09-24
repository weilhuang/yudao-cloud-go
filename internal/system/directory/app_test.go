package directory

import "testing"

func TestWebsitePattern(t *testing.T) {
	for _, site := range []string{"www.iocoder.cn", "localhost:48080", "a.b"} {
		if !websitePattern.MatchString(site) {
			t.Fatalf("应接受 %s", site)
		}
	}
	for _, site := range []string{"", "https://www.iocoder.cn", "bad host", "a/b"} {
		if websitePattern.MatchString(site) {
			t.Fatalf("应拒绝 %s", site)
		}
	}
}
