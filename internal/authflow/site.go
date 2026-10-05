package authflow

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/zriyox/qianjue-cli/internal/clierr"
)

// siteHost reduces a user-supplied site ("acme.example.com",
// "https://ACME.example.com/", "acme.example.com:3000") to its lowercase host.
func siteHost(site string) string {
	value := strings.TrimSpace(site)
	if i := strings.Index(value, "://"); i >= 0 {
		value = value[i+3:]
	}
	if i := strings.IndexAny(value, "/?#"); i >= 0 {
		value = value[:i]
	}
	if host, _, found := strings.Cut(value, ":"); found {
		value = host
	}
	return strings.ToLower(strings.TrimSuffix(value, "."))
}

// requirePageOnSite fails when a site was requested but the server did not
// honor it. A server that echoes the accepted site has validated it and chose
// the page itself (the official site's page host may differ from what the user
// typed, e.g. www vs apex), so the echo must equal the request. Without an echo
// (older server) the page host itself must be the site; only the host is
// compared, since the page is rendered from the site's URL template, which may
// carry a scheme or port the user did not type.
func requirePageOnSite(authorizationURL, echoedSite, site string) error {
	if strings.TrimSpace(site) == "" {
		return nil
	}
	want := siteHost(site)
	if strings.TrimSpace(echoedSite) != "" {
		if want != "" && siteHost(echoedSite) == want {
			return nil
		}
		return siteMismatch(want)
	}
	parsed, err := url.Parse(authorizationURL)
	if err == nil && want != "" && strings.EqualFold(parsed.Hostname(), want) {
		return nil
	}
	return siteMismatch(want)
}

func siteMismatch(want string) error {
	return clierr.New(clierr.KindServer, fmt.Sprintf(
		"服务端返回的授权页不在站点 %s 上（可能是服务端版本过旧、不支持 --site），为避免以其它站点账号登录已中止；"+
			"请升级服务端，或在该站点网页端创建 PAT 后用 auth import-token 登录", want))
}
