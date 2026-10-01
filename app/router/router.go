package router

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/memstore"
	"github.com/gin-gonic/gin"
	"github.com/v03413/bepusdt/app/access"
	"github.com/v03413/bepusdt/app/conf"
	"github.com/v03413/bepusdt/app/log"
	"github.com/v03413/bepusdt/app/model"
)

var engine *gin.Engine
var authRoute = make(map[string]bool)
var secureRoute = make(map[string]struct{})

func Handler() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)

	engine = gin.New()
	session := memstore.NewStore([]byte(model.GetK(model.AdminSecret)))
	session.Options(sessions.Options{
		MaxAge:   86400,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Path:     "/",
	})

	engine.Use(sessions.Sessions("session", session))
	engine.Use(gin.LoggerWithWriter(log.GetWriter()), gin.Recovery())
	engine.Use(sessionAuth(), copyright())
	engine.NoRoute(noRoute())
	engine.GET("/", func(ctx *gin.Context) {
		if !model.IsInstalled() {
			model.InstallLock()
			ctx.HTML(200, "installed.html", model.GetInstallInfo())

			return
		}

		if access.ValidGate(ctx) {
			ctx.HTML(200, "secure.html", gin.H{})
			return
		}

		if url := model.GetC(model.HomeRedirectUrl); url != "" {
			ctx.Redirect(302, url)
			return
		}

		ctx.HTML(200, "index.html", gin.H{"title": conf.Desc, "url": conf.Github})
	})

	{
		staticInit(engine)
		epusdtInit(engine)
		epayInit(engine)
		adminInit(engine)
		authInit(engine)
	}

	return engine
}

func sessionAuth() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var route = fmt.Sprintf("%s.%s", ctx.Request.Method, ctx.Request.URL.Path)
		// Fail closed for future routes in administrator namespaces unless explicitly registered.
		for _, prefix := range []string{"/api/auth", "/api/conf", "/api/wallet", "/api/order", "/api/rate", "/api/dashboard"} {
			if ctx.Request.URL.Path == prefix || strings.HasPrefix(ctx.Request.URL.Path, prefix+"/") {
				if _, registered := authRoute[route]; !registered {
					ctx.AbortWithStatusJSON(403, gin.H{"code": 403, "msg": "unauthorized access"})
					return
				}
			}
		}
		if _, ok := secureRoute[route]; ok {
			if !access.ValidGate(ctx) {
				ctx.JSON(403, gin.H{"code": 403, "msg": "unauthorized access"})
				ctx.Abort()
				return
			}
		}

		var need, ok = authRoute[route]
		if !ok || !need {
			ctx.Next()
			return
		}

		authHeader := ctx.GetHeader("Authorization")
		if authHeader == "" {
			ctx.JSON(403, gin.H{"code": 403, "msg": "missing authorization token"})
			ctx.Abort()
			return
		}

		if !access.Default.Verify(authHeader, sessions.Default(ctx).ID()) {
			ctx.JSON(403, gin.H{"code": 403, "msg": "invalid authorization token"})
			ctx.Abort()
			return
		}

		ctx.Next()
	}
}

func noRoute() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if ctx.Request.Method == http.MethodGet && ctx.Request.URL.Path == model.GetC(model.AdminSecure) {
			if err := access.RotateSession(ctx); err != nil {
				ctx.AbortWithStatus(http.StatusInternalServerError)
				return
			}

			ctx.Redirect(302, "/#/login")

			return
		}
	}
}

func copyright() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Writer.Header().Set("Payment-Gateway", "https://github.com/v03413/BEpusdt")
	}
}

func PostRegister(router *gin.RouterGroup, relativePath string, checkAuth bool, handlers ...gin.HandlerFunc) {
	var route = fmt.Sprintf("POST.%s%s", router.BasePath(), relativePath)

	authRoute[route] = checkAuth
	secureRoute[route] = struct{}{}

	router.POST(relativePath, handlers...)
}

func GetRegister(router *gin.RouterGroup, relativePath string, checkAuth bool, handlers ...gin.HandlerFunc) {
	var route = fmt.Sprintf("GET.%s%s", router.BasePath(), relativePath)

	authRoute[route] = checkAuth
	secureRoute[route] = struct{}{}

	router.GET(relativePath, handlers...)
}
