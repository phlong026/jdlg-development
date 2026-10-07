package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"unicode/utf8"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"service/component"
	"service/component/virtualpay"
	"service/component/wechatpay"
	"service/config"
	"service/function"
	"service/model"
	"service/processor"
	"service/route"
)

// createDefaultAdmin 创建默认管理员账号（如果不存在）
func createDefaultAdmin(userModel *model.UserModel) error {
	// 检查是否已存在 admin 用户
	_, err := userModel.GetByUsername("admin")
	if err == nil {
		// 用户已存在，跳过创建
		log.Println("默认管理员账号已存在")
		return nil
	}

	// 如果错误不是"未找到记录"，说明是其他错误，返回
	if err != sql.ErrNoRows {
		return err
	}

	// Never seed a publicly known password on a fresh database.
	initialPassword := os.Getenv("INITIAL_ADMIN_PASSWORD")
	if utf8.RuneCountInString(initialPassword) < 12 || len(initialPassword) > 72 {
		return fmt.Errorf("首次初始化请配置 INITIAL_ADMIN_PASSWORD（12 字符以上且不超过 72 字节）")
	}
	hash, hashErr := function.HashManagementPassword(initialPassword)
	if hashErr != nil {
		return hashErr
	}
	// 用户不存在，创建默认管理员
	admin := &model.User{
		Username: "admin",
		Password: hash,
		UserType: "management",
	}

	if err := userModel.Create(admin); err != nil {
		return err
	}

	log.Println("默认管理员账号创建成功：用户名 admin（初始密码已由环境变量配置）")
	return nil
}

func main() {
	if err := config.LoadLocalEnv(); err != nil {
		log.Fatalf("读取本地环境配置失败: %v", err)
	}
	if handled, err := maybeRunDataMigrationCommand(); err != nil {
		log.Fatalf("数据迁移执行失败: %v", err)
	} else if handled {
		return
	}

	logWriter, err := component.InitDailyAppLogger()
	if err != nil {
		log.Fatalf("初始化日志输出失败: %v", err)
	}
	defer logWriter.Close()
	log.Printf("应用日志已切换到按天文件: %s", logWriter.Directory())

	// 1. 初始化配置
	cfg := config.Init()

	// 打印微信支付关键配置，便于排查“client not inited”
	log.Printf("WechatPay config -> MchID=%q, CertDir=%q, NotifyURL=%q, APIv3Key_len=%d, Enabled=%v",
		cfg.WechatPay.MchID, cfg.WechatPay.CertDir, cfg.WechatPay.NotifyURL, len(cfg.WechatPay.APIv3Key), cfg.WechatPay.Enabled)

	// 2. 初始化COS客户端（使用长期密钥）
	if err := component.HealthCheck(cfg); err != nil {
		log.Printf("COS配置检查警告: %v", err)
	} else {
		if _, err := component.InitCOSClient(cfg, nil); err != nil {
			log.Printf("初始化COS客户端警告: %v", err)
		} else {
			log.Printf("COS客户端已初始化，存储桶: %s，地域: %s，前缀: %s，STS启用: %v", cfg.COS.Bucket, cfg.COS.Region, cfg.COS.Prefix, cfg.COS.EnableSTS)
		}
	}

	// 3. 初始化MySQL
	db, err := component.InitMySQL(cfg)
	if err != nil {
		log.Fatalf("初始化 MySQL 失败: %v\n提示: 请检查环境变量 MYSQL_DSN 是否正确配置，格式: user:password@tcp(host:port)/database?charset=utf8mb4&parseTime=True&loc=Local", err)
	}
	defer db.Close()
	log.Println("MySQL 连接成功")

	// 3.1. 启动时执行白名单内的数据迁移（fail-soft：失败不阻塞启动，仅记录日志）
	runAutoStartupMigrations(context.Background(), db)

	// 4. 初始化Redis客户端
	rdb, err := component.InitRedis(cfg)
	if err != nil {
		log.Fatalf("初始化 Redis 失败: %v", err)
	}

	// 5. 初始化用户表
	userDBModel := model.NewUserModel(db)
	if err := userDBModel.InitTable(); err != nil {
		log.Fatalf("初始化用户表失败: %v", err)
	}

	// 5.1. 创建默认管理员账号
	if err := createDefaultAdmin(userDBModel); err != nil {
		log.Printf("创建默认管理员警告: %v", err)
	}

	// 6. 初始化code_session表
	codeSessionModel := model.NewCodeSessionRedisModel(db, rdb)
	if err := codeSessionModel.InitTable(); err != nil {
		log.Fatalf("初始化 code_session 表失败: %v", err)
	}

	// 7. 初始化用户余额
	userModel := model.NewUserRedisModel(db, rdb)
	if err := userModel.InitStonesColumn(); err != nil {
		log.Printf("初始化用户余额字段警告: %v", err)
	}

	// 8. 初始化AI计费配置表
	pricingModel := model.NewAIPricingModel(db, rdb)
	if err := pricingModel.InitTable(); err != nil {
		log.Fatalf("初始化 ai_pricing 表失败: %v", err)
	}

	// 9. 初始化AI任务表
	taskModel := model.NewAITaskModel(db)
	if err := taskModel.InitTable(); err != nil {
		log.Fatalf("初始化 ai_tasks 表失败: %v", err)
	}

	// 10. 初始化AI API配置表
	apiConfigModel := model.NewAIAPIConfigModel(db, rdb)
	if err := apiConfigModel.InitTable(); err != nil {
		log.Fatalf("初始化 ai_api_config 表失败: %v", err)
	}
	// 插入默认AI API配置（如果不存在），Key 来自各供应商专用环境变量
	if err := apiConfigModel.InitDefaultConfigs(cfg.AI.LaoZhangAPIKey, cfg.AI.ToAPIsAPIKey); err != nil {
		log.Printf("初始化默认AI API配置失败: %v", err)
	}

	// 10.1. 初始化OSS文件表
	ossFileModel := model.NewOSSFileModel(db)
	if err := ossFileModel.InitTable(); err != nil {
		log.Fatalf("初始化 oss_files 表失败: %v", err)
	}

	// 10.2. 初始化日志配置表
	logConfigModel := model.NewLogConfigModel(db)
	if err := logConfigModel.InitTable(); err != nil {
		log.Fatalf("初始化 log_config 表失败: %v", err)
	}

	// 10.3. 初始化日志表
	logModel := model.NewLogModel(db)
	if err := logModel.InitTable(); err != nil {
		log.Fatalf("初始化 logs 表失败: %v", err)
	}

	// 10.4. 初始化签到记录表
	checkinModel := model.NewCheckinModel(db)
	if err := checkinModel.InitTable(); err != nil {
		log.Fatalf("初始化 checkin_records 表失败: %v", err)
	}

	// 10.5. 初始化模板表
	templateModel := model.NewTemplateModel(db)
	if err := templateModel.InitTable(); err != nil {
		log.Fatalf("初始化 templates 表失败: %v", err)
	}
	// 10.5.1. 初始化模板分类表
	templateCategoryModel := model.NewTemplateCategoryModel(db)
	if err := templateCategoryModel.InitTable(); err != nil {
		log.Fatalf("初始化 template_categories 表失败: %v", err)
	}
	// 10.5.2. 初始化模板解锁表
	templateUnlockModel := model.NewTemplateUnlockModel(db)
	if err := templateUnlockModel.InitTable(); err != nil {
		log.Fatalf("初始化 template_unlocks 表失败: %v", err)
	}

	// 10.6. 初始化用户扩展信息表
	userProfileModel := model.NewUserProfileModel(db)
	if err := userProfileModel.InitTable(); err != nil {
		log.Fatalf("初始化 user_profiles 表失败: %v", err)
	}
	// 10.6.1. 初始化用户身份凭证表（手机号、微信等）
	userIdentityModel := model.NewUserIdentityModel(db)
	if err := userIdentityModel.InitTable(); err != nil {
		log.Fatalf("初始化 user_identities 表失败: %v", err)
	}

	// 10.6.2. 自动迁移：将企业微信授权的手机号同步到 user_identities 表
	// 避免老用户通过手机号登录时因 user_identities 无记录而创建重复账号
	if applied, err := runEmbeddedDataMigration(context.Background(), db, "sync_enterprise_wechat_phones_to_identities.sql"); err != nil {
		log.Printf("企微手机号同步迁移执行警告: %v", err)
	} else if applied {
		log.Println("企微手机号同步迁移已执行：enterprise_wechat_contact → user_identities + user_profiles.phone")
	}

	// 10.7. 初始化灵石明细表
	stoneRecordModel := model.NewStoneRecordModel(db)
	if err := stoneRecordModel.InitTable(); err != nil {
		log.Fatalf("初始化 stone_records 表失败: %v", err)
	}

	// 10.8. 初始化用户订单表（充值/消费/文创/提现）
	userOrderModel := model.NewUserOrderModel(db)
	if err := userOrderModel.InitTable(); err != nil {
		log.Fatalf("初始化 user_orders 表失败: %v", err)
	}

	// 10.8.1. 初始化虚拟支付消息推送日志表（推送幂等去重 + 道具发货异步重试追踪）
	// 注：阶段 3.1 实现 /api/v1/wxmsg/push 路由时再注入到路由层使用
	if err := model.NewXPayMessageLogModel(db).InitTable(); err != nil {
		log.Fatalf("初始化 xpay_message_logs 表失败: %v", err)
	}

	// 10.9. 初始化邀请关系表
	inviteRelationModel := model.NewInviteRelationModel(db)
	if err := inviteRelationModel.InitTable(); err != nil {
		log.Fatalf("初始化 invite_relations 表失败: %v", err)
	}
	// 10.10. 初始化用户邀请码表（6 位数字+字母唯一）
	userInviteCodeModel := model.NewUserInviteCodeModel(db)
	if err := userInviteCodeModel.InitTable(); err != nil {
		log.Fatalf("初始化 user_invite_codes 表失败: %v", err)
	}
	// 10.11. 初始化模板点赞记录表（每人每模板只能点赞一次，可取消）
	templateLikeModel := model.NewTemplateLikeModel(db)
	if err := templateLikeModel.InitTable(); err != nil {
		log.Fatalf("初始化 template_likes 表失败: %v", err)
	}
	// 10.11.1. 初始化模板评论记录表
	templateCommentModel := model.NewTemplateCommentModel(db)
	if err := templateCommentModel.InitTable(); err != nil {
		log.Fatalf("初始化 template_comments 表失败: %v", err)
	}
	// 10.11.2. 初始化模板分享记录表
	templateShareModel := model.NewTemplateShareModel(db)
	if err := templateShareModel.InitTable(); err != nil {
		log.Fatalf("初始化 template_shares 表失败: %v", err)
	}
	// 10.12. 初始化模板广场双重 Tab 配置表
	templateSquareConfigModel := model.NewTemplateSquareConfigModel(db)
	if err := templateSquareConfigModel.InitTable(); err != nil {
		log.Fatalf("初始化 template_square_config 表失败: %v", err)
	}
	// 10.13. 初始化精选案例组表
	featuredCaseGroupModel := model.NewFeaturedCaseGroupModel(db)
	if err := featuredCaseGroupModel.InitTable(); err != nil {
		log.Fatalf("初始化 featured_case_groups 表失败: %v", err)
	}
	// 10.14. 初始化灵感素材表
	inspirationModel := model.NewInspirationAssetModel(db)
	if err := inspirationModel.InitTable(); err != nil {
		log.Fatalf("初始化 inspiration_assets 表失败: %v", err)
	}
	// 10.15. 初始化实用工具内容表
	utilityToolModel := model.NewUtilityToolModel(db)
	if err := utilityToolModel.InitTable(); err != nil {
		log.Fatalf("初始化 utility_tools 表失败: %v", err)
	}
	// 10.15.1. 初始化 AI 工具表
	aiToolModel := model.NewAIToolModel(db)
	if err := aiToolModel.InitTable(); err != nil {
		log.Fatalf("初始化 ai_tools 表失败: %v", err)
	}
	// 10.16. 初始化充值配置表
	rechargeConfigModel := model.NewRechargeConfigModel(db)
	if err := rechargeConfigModel.InitTable(); err != nil {
		log.Fatalf("初始化 recharge_config 表失败: %v", err)
	}
	membershipPlanModel := model.NewMembershipPlanModel(db)
	if err := membershipPlanModel.InitTable(); err != nil {
		log.Fatalf("初始化 membership_plans 表失败: %v", err)
	}
	userMembershipModel := model.NewUserMembershipModel(db)
	if err := userMembershipModel.InitTable(); err != nil {
		log.Fatalf("初始化 user_memberships 表失败: %v", err)
	}
	supportTicketModel := model.NewSupportTicketModel(db)
	if err := supportTicketModel.InitTable(); err != nil {
		log.Fatalf("初始化 support_tickets 表失败: %v", err)
	}
	customerServiceModel := model.NewCustomerServiceModel(db)
	if err := customerServiceModel.InitTable(); err != nil {
		log.Fatalf("初始化 customer_service 表失败: %v", err)
	}
	// 10.17. 初始化 AI 生成视频任务表
	videoTaskModel := model.NewAIVideoTaskModel(db)
	if err := videoTaskModel.InitTable(); err != nil {
		log.Fatalf("初始化 ai_video_tasks 表失败: %v", err)
	}
	// 10.17.1. 初始化企微绑定票据表（用于扫码添加企微后的自动回调绑定）
	enterpriseWechatBindTicketModel := model.NewEnterpriseWechatBindTicketModel(db)
	if err := enterpriseWechatBindTicketModel.InitTable(); err != nil {
		log.Fatalf("初始化 enterprise_wechat_bind_tickets 表失败: %v", err)
	}
	// 10.18. 初始化认证申请表（工单：个人设计师/企业认证，阿里云基础核验后由管理员审核）
	certificationApplicationModel := model.NewCertificationApplicationModel(db)
	if err := certificationApplicationModel.InitTable(); err != nil {
		log.Fatalf("初始化 certification_applications 表失败: %v", err)
	}
	designerReviewModel := model.NewDesignerReviewModel(db)
	if err := designerReviewModel.InitTable(); err != nil {
		log.Fatalf("初始化 designer_reviews 表失败: %v", err)
	}
	designerFollowModel := model.NewDesignerFollowModel(db)
	if err := designerFollowModel.InitTable(); err != nil {
		log.Fatalf("初始化 designer_follows 表失败: %v", err)
	}
	buildingCodeDocumentModel := model.NewBuildingCodeDocumentModel(db)
	if err := buildingCodeDocumentModel.InitTable(); err != nil {
		log.Fatalf("初始化 building_code_documents 表失败: %v", err)
	}
	buildingCodeChatModel := model.NewBuildingCodeChatModel(db)
	if err := buildingCodeChatModel.InitTable(); err != nil {
		log.Fatalf("初始化 building_code_chat_sessions/messages 表失败: %v", err)
	}

	// 11. 从MySQL加载数据到Redis
	log.Println("从 MySQL 加载 code_sessions 至 Redis 中...")
	if err := codeSessionModel.LoadFromMySQL(); err != nil {
		log.Printf("加载 code_sessions 警告: %v（继续运行）", err)
	}

	log.Println("从 MySQL 加载用户灵石余额至 Redis 中...")
	if err := userModel.LoadStonesFromMySQL(); err != nil {
		log.Printf("加载用户灵石余额警告: %v（继续运行）", err)
	}

	// 12. 初始化加密模块（SM2+SM4）
	if err := function.InitCrypto(cfg.Server.SM2PrivateKey, cfg.Server.SM2PublicKey, cfg.Server.SM4Key); err != nil {
		log.Printf("初始化加密模块警告: %v（token 加解密可能无法工作）", err)
	}
	// 密钥仍在用代码内置默认值时强警告。不直接拒绝启动：SM4/HMAC 密钥与小程序端共享，
	// 单方面换钥会让全部小程序请求验签失败，必须双端同步发版后才能切换（见 docs 安全方案）。
	if cfg.Server.SM4Key == "" {
		log.Println("[安全][严重] SM4_KEY 未配置，正在使用可从公开代码推导的内置默认密钥！" +
			"小程序 token 加解密与请求验签形同虚设。部署前请配置 SM4_KEY 并与小程序端同步切换（见 env.example）。")
	}

	// 12.1. 初始化微信支付（证书目录默认 service/certificate，APIv3 密钥可从 certificate/APIv3密钥.md 读取）
	wpCfg := wechatpay.Config{
		MchID:     cfg.WechatPay.MchID,
		APIv3Key:  cfg.WechatPay.APIv3Key,
		CertDir:   cfg.WechatPay.CertDir,
		NotifyURL: cfg.WechatPay.NotifyURL,
		Enabled:   cfg.WechatPay.Enabled,
	}
	if err := wechatpay.Init(context.Background(), wpCfg); err != nil {
		log.Printf("初始化微信支付警告: %v（请配置 WECHAT_PAY_MCH_ID、WECHAT_PAY_CERT_DIR、WECHAT_PAY_NOTIFY_URL 并设置 WECHAT_PAY_ENABLED=true）", err)
	}

	// 12.2. 初始化虚拟支付（mp 后台「虚拟支付」开通后获取 OfferID/AppKey/MessageToken/MessageAESKey）
	virtualpay.Init(virtualpay.Config{
		Enabled:               cfg.VirtualPay.Enabled,
		OfferID:               cfg.VirtualPay.OfferID,
		AppKeyProd:            cfg.VirtualPay.AppKeyProd,
		AppKeySandbox:         cfg.VirtualPay.AppKeySandbox,
		MessageToken:          cfg.VirtualPay.MessageToken,
		MessageEncodingAESKey: cfg.VirtualPay.MessageEncodingAESKey,
		IOSEnabled:            cfg.VirtualPay.IOSEnabled,
		SandboxMode:           cfg.VirtualPay.SandboxMode,
		CoinRatio:             cfg.VirtualPay.CoinRatio,
	})

	// 13. 初始化Redis session store
	store, err := component.InitSessionStore(cfg)
	if err != nil {
		log.Fatalf("初始化 session 存储失败: %v", err)
	}

	// 14. 初始化认证处理器（策略模式）
	authProcessor := processor.NewAuthProcessor()

	// 注册微信登录策略（按真实微信身份识别用户，支持同账号多设备登录）
	wechatStrategy := processor.NewWechatAuthStrategyWithProfile(userDBModel, codeSessionModel, userProfileModel, cfg.Wechat.AppID, cfg.Wechat.AppSecret)
	authProcessor.RegisterStrategy(wechatStrategy)

	// 注册账号密码登录策略（管理后台）
	passwordStrategy := processor.NewPasswordAuthStrategy(userDBModel, "management", nil)
	authProcessor.RegisterStrategy(passwordStrategy)

	// 注册手机号验证码登录策略
	phoneStrategy := processor.NewPhoneAuthStrategy(userDBModel, userProfileModel, codeSessionModel, userIdentityModel)
	authProcessor.RegisterStrategy(phoneStrategy)

	// 6. 初始化错误处理器
	route.InitErrorHandler(cfg)

	// 16. 初始化AI任务处理器（自动轮询并处理待处理任务）
	// 自动根据系统CPU和内存资源计算最优配置
	requestPool := component.InitRequestPoolAuto(cfg)
	log.Printf("HTTP请求池初始化完成: %v", requestPool.GetStats())

	// 16. 初始化AI任务处理器（自动轮询并处理待处理任务）
	component.InitAITaskProcessor(taskModel, apiConfigModel, userModel, stoneRecordModel, requestPool, cfg)
	route.ResumePendingAIVideoTasks(videoTaskModel, userModel, pricingModel, stoneRecordModel, supportTicketModel, cfg)

	// 7. 初始化Gin路由
	r := gin.New()
	r.Use(gin.LoggerWithConfig(gin.LoggerConfig{
		SkipPaths: []string{
			"/api/v1/management/wecom/callback",
			"/api/v1/miniprogram/ai/task/status",
		},
		Output: gin.DefaultWriter,
	}))
	r.Use(gin.Recovery())

	// 添加CORS中间件
	r.Use(route.CORSMiddleware())

	// 使用session中间件，cookie名设置为sessionid
	r.Use(sessions.Sessions("sessionid", store))

	// 注册所有路由
	stopReports := route.RegisterRoutes(r, authProcessor, codeSessionModel, userModel, pricingModel, taskModel, apiConfigModel, templateModel, inspirationModel, templateCategoryModel, templateSquareConfigModel, templateUnlockModel, templateLikeModel, templateCommentModel, templateShareModel, userProfileModel, stoneRecordModel, userOrderModel, inviteRelationModel, userInviteCodeModel, featuredCaseGroupModel, utilityToolModel, aiToolModel, rechargeConfigModel, membershipPlanModel, userMembershipModel, supportTicketModel, customerServiceModel, videoTaskModel, certificationApplicationModel, designerReviewModel, designerFollowModel, buildingCodeDocumentModel, buildingCodeChatModel, cfg)
	defer stopReports()

	// 8. 启动服务
	// 打印环境信息
	envName := "开发环境"
	if cfg.IsProduction() {
		envName = "生产环境"
	}

	log.Println("========================================")
	log.Printf("服务器启动成功！")
	log.Printf("当前环境: %s", envName)
	log.Printf("监听地址: %s", cfg.Server.Addr)
	log.Println("========================================")

	if err := r.Run(cfg.Server.Addr); err != nil {
		log.Fatalf("服务器启动失败: %v", err)
	}
}
