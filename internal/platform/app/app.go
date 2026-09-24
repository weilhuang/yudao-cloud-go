// Package app 把配置、MySQL、Redis、HTTP 和可选的 Nacos 注册装成一个进程。
package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/weilhuang/yudao-cloud-go/internal/infra/apilog"
	"github.com/weilhuang/yudao-cloud-go/internal/infra/codegen"
	infraconfig "github.com/weilhuang/yudao-cloud-go/internal/infra/config"
	"github.com/weilhuang/yudao-cloud-go/internal/infra/datasource"
	"github.com/weilhuang/yudao-cloud-go/internal/infra/demo01"
	"github.com/weilhuang/yudao-cloud-go/internal/infra/demo02"
	"github.com/weilhuang/yudao-cloud-go/internal/infra/demo03"
	"github.com/weilhuang/yudao-cloud-go/internal/infra/file"
	"github.com/weilhuang/yudao-cloud-go/internal/infra/redismonitor"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/cache"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/config"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/db"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/job"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/nacos"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/ws"
	"github.com/weilhuang/yudao-cloud-go/internal/system/area"
	"github.com/weilhuang/yudao-cloud-go/internal/system/auth"
	"github.com/weilhuang/yudao-cloud-go/internal/system/captcha"
	"github.com/weilhuang/yudao-cloud-go/internal/system/directory"
	"github.com/weilhuang/yudao-cloud-go/internal/system/identity"
	"github.com/weilhuang/yudao-cloud-go/internal/system/logger"
	"github.com/weilhuang/yudao-cloud-go/internal/system/message"
	"github.com/weilhuang/yudao-cloud-go/internal/system/permissionrpc"
	"github.com/weilhuang/yudao-cloud-go/internal/system/rolepostrpc"
	"github.com/weilhuang/yudao-cloud-go/internal/system/sendrpc"
	"github.com/weilhuang/yudao-cloud-go/internal/system/tenantrpc"
)

// Server 是已经在监听的进程。Shutdown 可重复调用。
type Server struct {
	url  string
	once sync.Once
	err  error
	stop func(context.Context) error
}

// URL 返回可访问的根地址，测试用它打 /health。
func (s *Server) URL() string {
	return s.url
}

// Shutdown 先注销 Nacos，再停 HTTP，最后关连接。
// 先注销是为了 Java 网关不要再把新请求打到正在退出的进程。
func (s *Server) Shutdown(ctx context.Context) error {
	s.once.Do(func() {
		s.err = s.stop(ctx)
	})
	return s.err
}

// Start 连上依赖后再监听端口。依赖失败时不对外提供 /health。
func Start(ctx context.Context, cfg config.Config) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	sqlDB, err := db.Open(ctx, cfg.MySQL.DSN)
	if err != nil {
		return nil, err
	}
	redisClient, err := cache.Open(ctx, cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB)
	if err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	// 必须在开始接受写请求前核对 Java Spring Cache 的可选前缀。
	javaRoleCache, err := directory.NewJavaRoleRedisCache(redisClient, os.Getenv("YUDAO_JAVA_CACHE_PREFIX"))
	if err != nil {
		_ = redisClient.Close()
		_ = sqlDB.Close()
		return nil, err
	}

	ln, err := net.Listen("tcp", cfg.HTTP.Addr)
	if err != nil {
		_ = redisClient.Close()
		_ = sqlDB.Close()
		return nil, fmt.Errorf("监听 %s: %w", cfg.HTTP.Addr, err)
	}

	gin.SetMode(gin.ReleaseMode)
	engine := httpx.NewRouter(cfg.App.Name)
	// 三个进程都连同一套库，所以都能校验令牌、写访问日志。登录入口只挂在单体和 system。
	account := &auth.MySQL{DB: sqlDB}
	sessions := &auth.Service{
		Users:       account,
		Tokens:      account,
		Permissions: account,
		Cache:       &auth.RedisCache{Client: redisClient},
		CaptchaOn:   cfg.Auth.CaptchaEnable,
		Sms:         account,
		CodeBegin:   cfg.Auth.EffectiveSmsCodeBegin(),
		CodeEnd:     cfg.Auth.EffectiveSmsCodeEnd(),
	}
	dirStore := &directory.MySQL{DB: sqlDB}
	dirSvc := &directory.Service{
		Reader: dirStore, Writer: dirStore, Depts: dirStore, Access: dirStore,
		Dicts: dirStore, Tenants: dirStore, JavaRoleCache: javaRoleCache,
		BcryptCost: cfg.Auth.BcryptCost,
	}
	permReader := &permissionrpc.MySQL{DB: sqlDB}
	permSvc := &permissionrpc.Service{Reader: permReader}
	dirSvc.DeptScope = func(ctx context.Context, tenantID, userID int64) (directory.UserAccess, error) {
		got, err := permSvc.GetDeptDataPermission(ctx, tenantID, userID)
		if err != nil {
			return directory.UserAccess{}, err
		}
		return directory.UserAccess{All: got.All, Self: got.Self, UserID: userID, DeptIDs: got.DeptIDs}, nil
	}
	sessions.ValidateTenant = func(ctx context.Context, tenantID int64, now time.Time) error {
		if err := dirSvc.ValidTenant(ctx, tenantID, now); err != nil {
			if biz, ok := err.(*directory.Error); ok {
				return &auth.Error{Code: biz.Code, Msg: biz.Msg}
			}
			return err
		}
		return nil
	}
	apiLogs := &apilog.MySQL{DB: sqlDB}
	engine.Use(apilog.Capture(cfg.App.Name, apiLogs, func(auditCtx context.Context, c *gin.Context) (int64, int, int64) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			return 0, 0, 0
		}
		token, err := sessions.Check(auditCtx, strings.TrimPrefix(header, "Bearer "))
		if err != nil || token == nil {
			return 0, 0, 0
		}
		return token.UserID, token.UserType, token.TenantID
	}))
	hub := ws.New()
	var broadcaster *ws.Broadcaster
	if cfg.App.Name == "yudao-server" || cfg.App.Name == "infra-server" {
		// 每个提供 WebSocket 的进程先确认 Redis 订阅，再开放 RPC 和 HTTP 入口。
		broadcaster, err = ws.StartBroadcaster(ctx, redisClient, hub)
		if err != nil {
			_ = ln.Close()
			_ = redisClient.Close()
			_ = sqlDB.Close()
			return nil, err
		}
		ws.Mount(engine, hub, func(r *http.Request) (int64, int64, int, bool) {
			token := r.URL.Query().Get("token")
			if token == "" {
				return 0, 0, 0, false
			}
			checked, err := sessions.Check(r.Context(), token)
			if err != nil || checked == nil {
				return 0, 0, 0, false
			}
			return checked.UserID, checked.TenantID, checked.UserType, true
		})
		// Java WebSocketSenderApi 对应内部 Feign 路由。用户与会话始终限定在请求租户。
		ws.MountRPC(engine, broadcaster, func(ctx context.Context, tenantID int64, now time.Time) (int, string) {
			if err := dirSvc.ValidTenant(ctx, tenantID, now); err != nil {
				var biz *directory.Error
				if errors.As(err, &biz) {
					return biz.Code, biz.Msg
				}
				return httpx.CodeInternal, "系统异常"
			}
			return httpx.CodeOK, ""
		})
	}
	if cfg.App.Name == "yudao-server" || cfg.App.Name == "system-server" {
		logStore := &logger.MySQL{DB: sqlDB}
		sessions.Recorder = logStore
		slider := &captcha.Service{Cache: captcha.Redis{Client: redisClient}}
		if cfg.Auth.CaptchaEnable {
			sessions.Captcha = slider
		}
		captcha.Mount(engine, slider)
		auth.Mount(engine, sessions)
		// Feign Token RPC 与管理端登录共用数据库和 Redis 中的令牌状态。
		auth.MountRPC(engine, sessions)
		accounts := &identity.MySQL{DB: sqlDB}
		// 社交授权 state 跨进程保存，并在回调时原子领取，避免伪造和重放。
		socials := &identity.Service{
			Store: accounts, StateStore: identity.RedisStateStore{Client: redisClient},
			AfterCommit: func(ctx context.Context, name string) error {
				return javaRoleCache.EvictNamedCache(ctx, name)
			},
		}
		socials.Exchange = socials.ExchangeCode
		socials.IssueAccess = func(ctx context.Context, tenantID, userID int64, clientID string, scopes []string) (string, time.Time, error) {
			token, err := sessions.CreateAccessToken(ctx, tenantID, userID, 2, clientID, scopes)
			if err != nil || token == nil {
				return "", time.Time{}, openAuthErr(err)
			}
			return token.AccessToken, token.ExpiresAt, nil
		}
		socials.Grants = identity.OpenGrants{
			Issue: func(ctx context.Context, tenantID, userID int64, userType int, clientID string, scopes []string) (identity.OpenToken, error) {
				token, err := sessions.CreateAccessToken(ctx, tenantID, userID, userType, clientID, scopes)
				return openToken(token), openAuthErr(err)
			},
			Refresh: func(ctx context.Context, refreshToken, clientID string) (identity.OpenToken, error) {
				token, err := sessions.RefreshTokenRPC(ctx, refreshToken, clientID, -1)
				return openToken(token), openAuthErr(err)
			},
			Password: func(ctx context.Context, tenantID int64, username, password, clientID string, scopes []string) (identity.OpenToken, error) {
				user, err := sessions.Authenticate(ctx, tenantID, username, password)
				if err != nil {
					return identity.OpenToken{}, openAuthErr(err)
				}
				token, err := sessions.CreateAccessToken(ctx, user.TenantID, user.ID, 2, clientID, scopes)
				return openToken(token), openAuthErr(err)
			},
			Check: func(ctx context.Context, accessToken string) (identity.OpenToken, error) {
				token, err := sessions.Check(ctx, accessToken)
				return openToken(token), openAuthErr(err)
			},
			Revoke: func(ctx context.Context, clientID, accessToken string) (bool, error) {
				token, err := sessions.Tokens.FindAccess(ctx, accessToken)
				if err != nil || token == nil || token.ClientID != clientID {
					return false, openAuthErr(err)
				}
				removed, err := sessions.RemoveAccessToken(ctx, token.TenantID, accessToken)
				return removed != nil, openAuthErr(err)
			},
		}
		identity.Mount(engine, sessions, socials, accounts)
		identity.MountSocialRPC(engine, socials, dirSvc.ValidTenant)
		dirSvc.OnNotice = func(tenantID int64, item directory.Notice) {
			raw, err := json.Marshal(item)
			if err != nil {
				return
			}
			// 站内公告与 Feign 发送走同一条跨实例通道；发送失败必须留下可定位的日志。
			publishCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := ws.Publish(publishCtx, redisClient, tenantID, "notice-push", string(raw)); err != nil {
				slog.Error("发布 WebSocket 公告失败", "tenantId", tenantID, "noticeId", item.ID, "err", err)
			}
		}
		directory.Mount(engine, sessions, dirSvc)
		dirSvc.RevokeAdminTokens = sessions.RevokeAdminTokens
		// 地区树不访问数据库；应用端免登录，管理端仍校验登录和租户。
		area.Mount(engine, sessions)
		// Java 模块通过 Feign 访问这组只读 RPC；模块模式仅由可信内部网络暴露。
		directory.MountRPC(engine, dirStore, dirSvc.ValidTenant, dirSvc.DeptScope)
		permissionrpc.MountRPC(engine, permSvc, dirSvc.ValidTenant)
		rolepostrpc.MountRPC(engine, &rolepostrpc.Service{Reader: &rolepostrpc.MySQL{DB: sqlDB}}, dirSvc.ValidTenant)
		// TenantCommonApi 带 @TenantIgnore，调用方无 tenant-id 也能查询并校验租户。
		tenantrpc.MountRPC(engine, &tenantrpc.Service{Reader: &tenantrpc.MySQL{DB: sqlDB}})
		logger.Mount(engine, sessions, logStore)
		messages := &message.MySQL{DB: sqlDB}
		smsSender := &message.Service{
			Store: messages,
			AfterCommit: func(ctx context.Context, name string) error {
				return javaRoleCache.EvictNamedCache(ctx, name)
			},
		}
		// 验证码先落库。渠道发送复用已有短信服务；未配置模板时不会访问云厂商。
		sessions.Sms = account
		sessions.Codes = codeSender{
			send: smsSender.SendSms,
			status: func(ctx context.Context, id int64) (int, error) {
				item, err := messages.SmsLogByID(ctx, id)
				if err != nil || item == nil {
					if err != nil {
						return 0, err
					}
					return 0, nil
				}
				status, _ := item["sendStatus"].(int)
				return status, nil
			},
		}
		message.Mount(engine, sessions, smsSender, messages)
		sendrpc.Mount(engine, sessions, smsSender, &sendrpc.MySQL{DB: sqlDB}, dirSvc.ValidTenant)
	}
	if cfg.App.Name == "yudao-server" || cfg.App.Name == "infra-server" {
		apilog.Mount(engine, sessions, apiLogs)
		files := &file.MySQL{DB: sqlDB}
		fileSvc := &file.Service{Store: files, Blobs: files}
		fileLimits := file.UploadLimits{
			MaxFileBytes:    cfg.HTTP.EffectiveUploadMaxFileBytes(),
			MaxRequestBytes: cfg.HTTP.EffectiveUploadMaxRequestBytes(),
		}
		file.Mount(engine, sessions, fileSvc, fileLimits)
		file.MountRPC(engine, fileSvc, fileLimits)
		infraconfig.Mount(engine, sessions, &infraconfig.MySQL{DB: sqlDB})
		datasource.Mount(engine, sessions, &datasource.Service{
			Store:  &datasource.MySQL{DB: sqlDB},
			Key:    cfg.MyBatis.EffectiveEncryptorPassword(),
			Master: datasource.MasterFromDSN(cfg.MySQL.DSN),
		})
		codegen.Mount(engine, sessions, &codegen.Service{
			DB:      sqlDB,
			Sources: &datasource.MySQL{DB: sqlDB},
			Key:     cfg.MyBatis.EffectiveEncryptorPassword(),
			Author: func(ctx context.Context, userID int64) (string, error) {
				user, err := sessions.Users.FindByID(ctx, userID)
				if err != nil || user == nil {
					return "", err
				}
				return user.Nickname, nil
			},
		})
		demo01.Mount(engine, sessions, &demo01.Service{DB: sqlDB})
		demo02.Mount(engine, sessions, &demo02.Service{DB: sqlDB})
		demo03.Mount(engine, sessions, &demo03.Service{DB: sqlDB})
		redismonitor.Mount(engine, sessions, redismonitor.RedisReader{Client: redisClient})
	}
	httpServer := newHTTPServer(engine, cfg.HTTP)
	go func() {
		err := httpServer.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("HTTP 服务退出", "err", err)
		}
	}()
	go job.Loop(ctx, 24*time.Hour, func(runCtx context.Context, now time.Time) error {
		_, err := job.Clean(runCtx, sqlDB, now)
		return err
	})

	port := ln.Addr().(*net.TCPAddr).Port
	var reg nacos.Registrar
	if cfg.Nacos.Enabled {
		// 监听成功后再注册，避免把一个还没接客的地址交给 Java 网关。
		if cfg.Nacos.RegisterPort == 0 {
			cfg.Nacos.RegisterPort = uint64(port)
		}
		reg, err = nacos.Register(cfg.Nacos)
		if err != nil {
			_ = httpServer.Close()
			broadcaster.Close()
			_ = redisClient.Close()
			_ = sqlDB.Close()
			return nil, err
		}
	}

	running := &Server{
		url: fmt.Sprintf("http://127.0.0.1:%d", port),
		stop: func(stopCtx context.Context) error {
			return shutdown(stopCtx, reg, httpServer, broadcaster, redisClient, sqlDB)
		},
	}
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = running.Shutdown(c)
	}()
	slog.Info("进程已启动", "name", cfg.App.Name, "addr", running.url, "nacos", cfg.Nacos.Enabled)
	return running, nil
}

func newHTTPServer(engine http.Handler, httpConfig config.HTTP) *http.Server {
	return &http.Server{
		Handler: engine,
		// 读取超时覆盖整个请求体，防止慢速上传长期占用连接；WebSocket 升级后由 Upgrader 清除连接期限。
		ReadTimeout: time.Duration(httpConfig.EffectiveReadTimeoutSeconds()) * time.Second,
		// 不设 WriteTimeout，避免中断升级后的 WebSocket 连接。
		ReadHeaderTimeout: 5 * time.Second,
	}
}

func shutdown(ctx context.Context, reg nacos.Registrar, httpServer *http.Server, broadcaster *ws.Broadcaster, redisClient *redis.Client, sqlDB *sql.DB) error {
	var joined error
	if reg != nil {
		if err := reg.Deregister(); err != nil {
			joined = errors.Join(joined, err)
		}
	}
	if err := httpServer.Shutdown(ctx); err != nil {
		joined = errors.Join(joined, fmt.Errorf("关闭 HTTP: %w", err))
	}
	broadcaster.Close()
	if err := redisClient.Close(); err != nil {
		joined = errors.Join(joined, fmt.Errorf("关闭 Redis: %w", err))
	}
	if err := sqlDB.Close(); err != nil {
		joined = errors.Join(joined, fmt.Errorf("关闭 MySQL: %w", err))
	}
	return joined
}

// Execute 启动进程并阻塞到 ctx 结束。启动失败返回 1，供 main 作为退出码。
func Execute(ctx context.Context, cfg config.Config) int {
	// 发现客户端供整个进程复用，只在进程真正退出时统一关闭。
	defer nacos.CloseDiscovery()
	srv, err := Start(ctx, cfg)
	if err != nil {
		slog.Error("启动失败", "err", err)
		return 1
	}
	<-ctx.Done()
	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(stopCtx); err != nil {
		slog.Error("关闭失败", "err", err)
		return 1
	}
	return 0
}

func openToken(token *auth.Token) identity.OpenToken {
	if token == nil {
		return identity.OpenToken{}
	}
	return identity.OpenToken{
		AccessToken: token.AccessToken, RefreshToken: token.RefreshToken, ExpiresAt: token.ExpiresAt,
		Scopes: token.Scopes, UserID: token.UserID, UserType: token.UserType, TenantID: token.TenantID, ClientID: token.ClientID,
	}
}

func openAuthErr(err error) error {
	if err == nil {
		return nil
	}
	if biz, ok := err.(*auth.Error); ok {
		return &identity.Error{Code: biz.Code, Msg: biz.Msg}
	}
	return err
}

// NotifyContext 在收到 SIGINT 或 SIGTERM 时取消，main 用它触发关闭。
func NotifyContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}
