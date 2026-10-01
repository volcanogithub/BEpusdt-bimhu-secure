package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/spf13/cast"
	"github.com/v03413/bepusdt/app/access"
	"github.com/v03413/bepusdt/app/handler/base"
	"github.com/v03413/bepusdt/app/model"
	"golang.org/x/crypto/bcrypt"
)

type Auth struct {
}

type authLoginReq struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type authPasswordReq struct {
	Password        string `json:"password" binding:"required"`
	NewPassword     string `json:"new_password" binding:"required"`
	ConfirmPassword string `json:"confirm_password" binding:"required"`
}

func (Auth) Info(ctx *gin.Context) {
	base.Ok(ctx, gin.H{
		"admin_username": model.GetK(model.AdminUsername),
		"trade_type":     model.GetAllAlias(),
		"trade_fiat":     model.GetSupportFiat(),
		"trade_crypto":   model.GetSupportCrypto(),
		"trade_network":  model.GetAllNetwork(),
	})
}

func (Auth) Menu(ctx *gin.Context) {
	type meta struct {
		Title     string   `json:"title"`
		Hide      bool     `json:"hide"`
		Disable   bool     `json:"disable"`
		KeepAlive bool     `json:"keepAlive"`
		Affix     bool     `json:"affix"`
		Link      string   `json:"link"`
		Iframe    bool     `json:"iframe"`
		IsFull    bool     `json:"isFull"`
		Roles     []string `json:"roles"`
		SvgIcon   string   `json:"svgIcon"`
		Icon      string   `json:"icon"`
		Sort      int      `json:"sort"`
		Type      int      `json:"type"`
	}
	type menu struct {
		Id        string `json:"id"`
		ParentId  string `json:"parentId"`
		Path      string `json:"path"`
		Name      string `json:"name"`
		Component string `json:"component"`
		Meta      meta   `json:"meta"`
		Children  []menu `json:"children"`
	}

	var data = []menu{
		{
			Id:        "01",
			Path:      "/home",
			Name:      "home",
			Component: "home/home",
			Meta: meta{
				Title:     "home",
				Hide:      false,
				Disable:   false,
				KeepAlive: false,
				Affix:     true,
				Link:      "",
				Iframe:    false,
				IsFull:    false,
				Roles:     []string{"admin"},
				SvgIcon:   "home",
				Icon:      "",
				Sort:      1,
				Type:      2,
			},
			Children: nil,
		},
		{
			Id:        "02",
			Path:      "/wallet",
			Name:      "wallet",
			Component: "wallet/wallet",
			Meta: meta{
				Title:     "wallet",
				Hide:      false,
				Disable:   false,
				KeepAlive: true,
				Affix:     false,
				Link:      "",
				Iframe:    false,
				IsFull:    false,
				Roles:     []string{"admin"},
				SvgIcon:   "classify",
				Icon:      "",
				Sort:      1,
				Type:      2,
			},
			Children: nil,
		},
		{
			Id:        "03",
			Path:      "/order",
			Name:      "order",
			Component: "order/order",
			Meta: meta{
				Title:     "order",
				Hide:      false,
				Disable:   false,
				KeepAlive: true,
				Affix:     false,
				Link:      "",
				Iframe:    false,
				IsFull:    false,
				Roles:     []string{"admin"},
				SvgIcon:   "table",
				Icon:      "",
				Sort:      1,
				Type:      2,
			},
			Children: nil,
		},
		{
			Id:        "04",
			Path:      "/rate",
			Name:      "rate",
			Component: "rate/rate",
			Meta: meta{
				Title:     "rate",
				Hide:      false,
				Disable:   false,
				KeepAlive: true,
				Affix:     false,
				Link:      "",
				Iframe:    false,
				IsFull:    false,
				Roles:     []string{"admin"},
				SvgIcon:   "directives",
				Icon:      "",
				Sort:      1,
				Type:      1,
			},
			Children: []menu{
				{
					Id:        "0401",
					ParentId:  "04",
					Path:      "/rate/list",
					Name:      "rate-list",
					Component: "rate/list",
					Meta: meta{
						Title:     "rate-list",
						Hide:      false,
						Disable:   false,
						KeepAlive: true,
						Affix:     false,
						Link:      "",
						Iframe:    false,
						IsFull:    false,
						Roles:     []string{"admin"},
						SvgIcon:   "",
						Icon:      "icon-list",
						Sort:      1,
						Type:      2,
					},
					Children: nil,
				},
				{
					Id:        "0402",
					ParentId:  "04",
					Path:      "/rate/syntax",
					Name:      "rate-syntax",
					Component: "rate/syntax",
					Meta: meta{
						Title:     "rate-syntax",
						Hide:      false,
						Disable:   false,
						KeepAlive: true,
						Affix:     false,
						Link:      "",
						Iframe:    false,
						IsFull:    false,
						Roles:     []string{"admin"},
						SvgIcon:   "",
						Icon:      "icon-settings",
						Sort:      1,
						Type:      2,
					},
					Children: nil,
				},
			},
		},
		{
			Id:        "05",
			Path:      "/system",
			Name:      "system",
			Component: "system/system",
			Meta: meta{
				Title:     "system",
				Hide:      false,
				Disable:   false,
				KeepAlive: true,
				Affix:     false,
				Link:      "",
				Iframe:    false,
				IsFull:    false,
				Roles:     []string{"admin"},
				SvgIcon:   "set",
				Icon:      "",
				Sort:      1,
				Type:      1,
			},
			Children: []menu{
				{
					Id:        "0501",
					ParentId:  "05",
					Path:      "/system/base/base",
					Name:      "system-base",
					Component: "system/base/base",
					Meta: meta{
						Title:     "system-base",
						Hide:      false,
						Disable:   false,
						KeepAlive: true,
						Affix:     false,
						Link:      "",
						Iframe:    false,
						IsFull:    false,
						Roles:     []string{"admin"},
						SvgIcon:   "",
						Icon:      "icon-layers",
						Sort:      1,
						Type:      2,
					},
					Children: nil,
				},
				{
					Id:        "0502",
					ParentId:  "05",
					Path:      "/system/rpc/rpc",
					Name:      "system-rpc",
					Component: "system/rpc/rpc",
					Meta: meta{
						Title:     "system-rpc",
						Hide:      false,
						Disable:   false,
						KeepAlive: true,
						Affix:     false,
						Link:      "",
						Iframe:    false,
						IsFull:    false,
						Roles:     []string{"admin"},
						SvgIcon:   "",
						Icon:      "icon-thunderbolt",
						Sort:      1,
						Type:      2,
					},
					Children: nil,
				},
			},
		},
		{
			Id:        "06",
			Path:      "/create-order",
			Name:      "create-order",
			Component: "order/create-order",
			Meta: meta{
				Title:     "create-order",
				Hide:      false,
				Disable:   false,
				KeepAlive: true,
				Affix:     false,
				Link:      "",
				Iframe:    false,
				IsFull:    false,
				Roles:     []string{"admin"},
				SvgIcon:   "form",
				Icon:      "",
				Sort:      1,
				Type:      2,
			},
			Children: nil,
		},
		{
			Id:        "07",
			Path:      "/github-api-doc",
			Name:      "github-api-doc",
			Component: "about/doc",
			Meta: meta{
				Title:     "github-api-doc",
				Hide:      false,
				Disable:   false,
				KeepAlive: true,
				Affix:     false,
				Link:      "",
				Iframe:    false,
				IsFull:    false,
				Roles:     []string{"admin"},
				SvgIcon:   "about",
				Icon:      "",
				Sort:      1,
				Type:      2,
			},
		},
	}

	base.Ok(ctx, data)
}

func (Auth) Login(ctx *gin.Context) {
	ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 4096)
	var req authLoginReq
	bindErr := ctx.ShouldBindJSON(&req)
	if !access.Default.Attempt(req.Username, access.ClientIP(ctx)) {
		ctx.Header("Retry-After", "300")
		ctx.JSON(http.StatusTooManyRequests, gin.H{"code": 429, "msg": "invalid credentials"})
		return
	}
	// Always check bcrypt, even for an unknown username, to avoid a cheap timing oracle.
	passwordErr := bcrypt.CompareHashAndPassword([]byte(model.GetK(model.AdminPassword)), []byte(req.Password))
	usernameHash := sha256.Sum256([]byte(req.Username))
	expectedHash := sha256.Sum256([]byte(model.GetK(model.AdminUsername)))
	if bindErr != nil || passwordErr != nil || subtle.ConstantTimeCompare(usernameHash[:], expectedHash[:]) != 1 {
		ctx.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "invalid credentials"})
		return
	}
	if err := access.RotateSession(ctx); err != nil {
		base.Error(ctx, err)
		return
	}
	token, err := access.Default.Issue(sessions.Default(ctx).ID())
	if err != nil {
		_ = access.InvalidateSession(ctx)
		base.Error(ctx, err)
		return
	}
	access.Default.Success(req.Username)

	model.SetK(model.AdminLoginIP, access.ClientIP(ctx))
	model.SetK(model.AdminLoginAt, cast.ToString(time.Now().Format(time.DateTime)))

	base.Response(ctx, 200, gin.H{"token": token, "types": model.GetAllAlias()})
}

func (Auth) Logout(ctx *gin.Context) {
	access.Default.Revoke()
	if err := access.InvalidateSession(ctx); err != nil {
		base.Error(ctx, err)
		return
	}

	base.Response(ctx, 200, "退出成功")
}

func (Auth) SetPassword(ctx *gin.Context) {
	var req authPasswordReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		base.BadRequest(ctx, err.Error())

		return
	}

	var password = model.GetK(model.AdminPassword)
	if bcrypt.CompareHashAndPassword([]byte(password), []byte(req.Password)) != nil {
		base.BadRequest(ctx, "原密码错误")

		return
	}

	if req.ConfirmPassword != req.NewPassword {
		base.BadRequest(ctx, "两次输入的新密码不一致")

		return
	}

	var newPassword = strings.TrimSpace(req.NewPassword)
	if len(newPassword) < 6 {
		base.BadRequest(ctx, "新密码长度不能少于6位")

		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		base.BadRequest(ctx, "invalid password")
		return
	}

	if err := model.SetSecretValues(map[model.ConfKey]string{model.AdminPassword: string(hash)}); err != nil {
		base.Error(ctx, err)
		return
	}
	access.Default.Revoke()
	if err := access.InvalidateSession(ctx); err != nil {
		base.Error(ctx, err)
		return
	}

	base.Ok(ctx, "修改成功，请重新登录")
}
