// Local-only integration harness. It reuses production login, authorization and CRM routes.
// It is not the platform production entry point and refuses any other database name.
package main

import (
	"database/sql"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	mysql "github.com/go-sql-driver/mysql"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"service/function"
	"service/model"
	"service/processor"
	"service/route"
	"strings"
	"time"
)

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
func main() {
	if os.Getenv("CRM_LOCAL_INTEGRATION") != "true" {
		log.Fatal("local integration mode must be explicit")
	}
	dsn := os.Getenv("MYSQL_DSN")
	cfg, err := mysql.ParseDSN(dsn)
	must(err)
	if cfg.DBName != "jd_crm_integration" {
		log.Fatal("only isolated jd_crm_integration database is allowed")
	}
	db, err := sql.Open("mysql", dsn)
	must(err)
	defer db.Close()
	must(db.Ping())
	users := model.NewUserModel(db)
	must(users.InitTable())
	leads := model.NewCustomerServiceModel(db)
	must(leads.InitTable())
	var tables int
	must(db.QueryRow(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name='management_access'`).Scan(&tables))
	if tables == 0 {
		data, err := os.ReadFile("/workspace/sql/customer_management_bridge_up.sql")
		must(err)
		lines := []string{}
		for _, l := range strings.Split(string(data), "\n") {
			if !strings.HasPrefix(strings.TrimSpace(l), "--") {
				lines = append(lines, l)
			}
		}
		for _, statement := range strings.Split(strings.Join(lines, "\n"), ";") {
			if strings.TrimSpace(statement) != "" {
				_, err = db.Exec(statement)
				must(err)
			}
		}
	}
	for _, seed := range []struct {
		id          int
		name, scope string
	}{{1, "crm_admin", "platform_admin"}, {2, "crm_advisor", "crm_only"}} {
		_, err = db.Exec(`INSERT IGNORE INTO users(id,username,password,openid,unionid,user_type) VALUES (?,?,?,NULL,NULL,'management')`, seed.id, seed.name, function.HashPassword("LocalCRM!2026"))
		must(err)
		_, err = db.Exec(`INSERT IGNORE INTO management_access(user_id,scope,enabled,version,updated_by,updated_at) VALUES (?,?,TRUE,0,1,NOW(6))`, seed.id, seed.scope)
		must(err)
	}
	auth := processor.NewAuthProcessor()
	auth.RegisterStrategy(processor.NewPasswordAuthStrategy(users, "management", nil))
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	store := cookie.NewStore([]byte("local-integration-session-key-2026"))
	store.Options(sessions.Options{Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 28800})
	r.Use(sessions.Sessions("JD_CRM_LOCAL_SESSION", store))
	management := r.Group("/api/v1/management")
	route.RegisterManagementRoutes(management, auth, users, nil, nil, nil)
	protected := management.Group("")
	protected.Use(route.AuthRequired, route.ManagementAccessRequired(users))
	route.RegisterCRMManagementRoutes(protected, users)
	route.RegisterCustomerLeadManagementRoutes(protected, leads)
	dist := os.Getenv("CRM_FRONTEND_DIST")
	if dist == "" {
		dist = "/frontend"
	}
	r.Static("/admin/assets", filepath.Join(dist, "assets"))
	r.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"mode": "local-integration", "time": time.Now()}) })
	r.NoRoute(func(c *gin.Context) {
		if c.Request.Method == "GET" && (c.Request.URL.Path == "/admin" || strings.HasPrefix(c.Request.URL.Path, "/admin/")) {
			c.File(filepath.Join(dist, "index.html"))
			return
		}
		c.JSON(404, gin.H{"code": 404, "msg": "此隔离环境只加载客户管理及原线索模块"})
	})
	log.Println("local CRM integration listening on :8080")
	must(r.Run(":8080"))
}
