package lg

import (
	"net/url"
	"testing"
)

func TestNormalizeMediaURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "strip junk query without ads",
			in:   "https://stream.example/news.m3u8?token=abc&exp=1",
			want: "https://stream.example/news.m3u8",
		},
		{
			name: "no query unchanged",
			in:   "https://stream.example/dup.m3u8",
			want: "https://stream.example/dup.m3u8",
		},
		{
			name: "hells kitchen keep ads neutralize macros",
			in: "https://d1bl6tskrpq9ze.cloudfront.net/hls/master.m3u8?" +
				"ads.xumo_channelId=99992260&ads.channelId=ch1&ads.ifa=[IFA]&ads.lmt=[LMT]&utm=1",
			want: mustEncodeQuery(
				"https://d1bl6tskrpq9ze.cloudfront.net/hls/master.m3u8",
				url.Values{
					"ads.xumo_channelId": {"99992260"},
					"ads.channelId":      {"ch1"},
					"ads.ifa":            {""},
					"ads.lmt":            {""},
				},
			),
		},
		{
			name: "ads only keeps ads keys",
			in:   "https://cdn.example/hls/master.m3u8?ads.channelId=abc&foo=bar",
			want: mustEncodeQuery("https://cdn.example/hls/master.m3u8", url.Values{
				"ads.channelId": {"abc"},
			}),
		},
		{
			name: "empty",
			in:   "  ",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeMediaURL(tt.in)
			if got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestNeutralizeClientMacros(t *testing.T) {
	if got := neutralizeClientMacros("prefix-[IFA]-suffix"); got != "prefix--suffix" {
		t.Fatalf("got %q", got)
	}
	if got := neutralizeClientMacros("[LMT]"); got != "" {
		t.Fatalf("got %q", got)
	}
}

func mustEncodeQuery(base string, q url.Values) string {
	u, err := url.Parse(base)
	if err != nil {
		panic(err)
	}
	u.RawQuery = q.Encode()
	return u.String()
}
