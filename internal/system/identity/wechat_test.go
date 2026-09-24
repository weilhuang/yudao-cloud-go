package identity

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/system/auth"
)

func TestMaskMobile(t *testing.T) {
	if got := maskMobile("13812345678"); got != "138****5678" {
		t.Fatal(got)
	}
	if got := maskMobile(""); got != "" {
		t.Fatal(got)
	}
}

func TestWxAPIUsesWechatAndKeepsFailures(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var uploads int
	var sent map[string]any
	var qr map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/cgi-bin/token"):
			_, _ = w.Write([]byte(`{"access_token":"token-1","expires_in":7200}`))
		case strings.Contains(r.URL.Path, "/ticket/getticket"):
			_, _ = w.Write([]byte(`{"errcode":0,"ticket":"ticket-1"}`))
		case strings.Contains(r.URL.Path, "/getuserphonenumber"):
			_, _ = w.Write([]byte(`{"errcode":0,"phone_info":{"phoneNumber":"13812345678","purePhoneNumber":"13812345678","countryCode":86}}`))
		case strings.Contains(r.URL.Path, "/getwxacodeunlimit"):
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &qr)
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("PNG"))
		case strings.Contains(r.URL.Path, "/newtmpl/gettemplate"):
			_, _ = w.Write([]byte(`{"errcode":0,"data":[{"priTmplId":"tmpl-1","title":"发货通知","content":"商品{{thing1.DATA}}","example":"示例","type":2}]}`))
		case strings.Contains(r.URL.Path, "/subscribe/send"):
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &sent)
			_, _ = w.Write([]byte(`{"errcode":0}`))
		case strings.Contains(r.URL.Path, "/upload_shipping_info"):
			uploads++
			raw, _ := io.ReadAll(r.Body)
			if uploads == 1 {
				if !strings.Contains(string(raw), "138****5678") || !strings.Contains(string(raw), `"order_number_type":2`) {
					t.Fatalf("发货请求：%s", raw)
				}
				_, _ = w.Write([]byte(`{"errcode":10060001,"errmsg":"支付单不存在"}`))
				return
			}
			_, _ = w.Write([]byte(`{"errcode":0}`))
		case strings.Contains(r.URL.Path, "/notify_confirm_receive"):
			raw, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(raw), `"received_time":1700000000`) {
				t.Fatalf("收货时间应按秒：%s", raw)
			}
			_, _ = w.Write([]byte(`{"errcode":0}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var slept []time.Duration
	svc := &Service{
		Store: &memStore{social: &SocialClient{SocialType: socialWechatMini, UserType: 1, ClientID: "wx-app", ClientSecret: "secret", Status: 0},
			boundSocial: &SocialUser{OpenID: "openid-1"}},
		WechatBase: server.URL, HTTP: server.Client(),
		WxNow:   func() time.Time { return time.Unix(1700000000, 0) },
		WxNonce: func() string { return "nonce-fixed-16" },
		WxSleep: func(d time.Duration) { slept = append(slept, d) },
	}
	missing := &Service{Store: &memStore{}, WechatBase: server.URL, HTTP: server.Client()}
	if _, err := missing.WxJsapiSignature(context.Background(), 1, 1, "https://app.example"); err == nil || err.(*Error).Code != 1_002_018_210 {
		t.Fatal(err)
	}

	sign, err := svc.WxJsapiSignature(context.Background(), 1, 1, "https://app.example/a")
	if err != nil || sign.AppID != "wx-app" || sign.Signature != expectSign("ticket-1", "nonce-fixed-16", "1700000000", "https://app.example/a") {
		t.Fatalf("%+v %v", sign, err)
	}
	phone, err := svc.WxPhoneNumber(context.Background(), 1, 1, "phone-code")
	if err != nil || phone.PurePhoneNumber != "13812345678" || phone.CountryCode != "86" {
		t.Fatalf("%+v %v", phone, err)
	}
	image, err := svc.WxaQrcode(context.Background(), 1, "1001", "pages/index", nil, nil, nil, nil)
	if err != nil || string(image) != "PNG" || qr["scene"] != "1001" || qr["page"] != "pages/index" || qr["env_version"] != "release" || qr["width"] != float64(430) {
		t.Fatalf("%s %+v %v", image, qr, err)
	}
	list, err := svc.WxaTemplates(context.Background(), 1, 1)
	if err != nil || len(list) != 1 || list[0].ID != "tmpl-1" || list[0].Title != "发货通知" {
		t.Fatalf("%+v %v", list, err)
	}
	ok, err := svc.SendWxaSubscribe(context.Background(), 1, WxaSubscribe{UserID: 7, UserType: 1, TemplateTitle: "不存在"})
	if err != nil || ok {
		t.Fatalf("缺少模板应返回 false：%v %v", ok, err)
	}
	ok, err = svc.SendWxaSubscribe(context.Background(), 1, WxaSubscribe{UserID: 7, UserType: 1, TemplateTitle: "发货通知", Page: "pages/index", Messages: map[string]string{"thing1": "书"}})
	if err != nil || !ok || sent["touser"] != "openid-1" || sent["template_id"] != "tmpl-1" || sent["miniprogram_state"] != "formal" {
		t.Fatalf("%v %v %+v", ok, err, sent)
	}
	if err := svc.UploadWxaShipping(context.Background(), 1, 1, WxaShipping{OpenID: "payer", TransactionID: "tx", LogisticsType: 1, LogisticsNo: "SF1", ExpressCompany: "SF", ItemDesc: "书", ReceiverContact: "13812345678"}); err != nil || uploads != 2 || len(slept) != 1 || slept[0] != time.Second {
		t.Fatalf("uploads=%d slept=%v err=%v", uploads, slept, err)
	}
	if err := svc.NotifyWxaConfirm(context.Background(), 1, 1, "tx", 1700000000000); err != nil {
		t.Fatal(err)
	}

	r := gin.New()
	MountSocialRPC(r, svc, nil)
	req := httptest.NewRequest(http.MethodGet, "/rpc-api/system/social-client/get-wxa-subscribe-template-list?userType=1", nil)
	req.Header.Set("tenant-id", "1")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "tmpl-1") {
		t.Fatal(rec.Body.String())
	}
	bad := httptest.NewRequest(http.MethodPost, "/rpc-api/system/social-client/send-wxa-subscribe-message", strings.NewReader(`{"userId":1,"userType":1}`))
	bad.Header.Set("Content-Type", "application/json")
	bad.Header.Set("tenant-id", "1")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, bad)
	if !strings.Contains(rec.Body.String(), "消息模版标题不能为空") {
		t.Fatal(rec.Body.String())
	}

	admin := gin.New()
	Mount(admin, &auth.Service{}, svc, nil)
	unauth := httptest.NewRequest(http.MethodPost, "/admin-api/system/social-client/send-subscribe-message", strings.NewReader(`{}`))
	rec = httptest.NewRecorder()
	admin.ServeHTTP(rec, unauth)
	if rec.Code == http.StatusNotFound || !strings.Contains(rec.Body.String(), "账号未登录") {
		t.Fatal(rec.Code, rec.Body.String())
	}
}

func expectSign(ticket, nonce, ts, page string) string {
	parts := []string{"jsapi_ticket=" + ticket, "noncestr=" + nonce, "timestamp=" + ts, "url=" + page}
	sort.Strings(parts)
	sum := sha1.Sum([]byte(strings.Join(parts, "&")))
	return hex.EncodeToString(sum[:])
}
