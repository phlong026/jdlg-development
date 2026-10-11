package route

import (
	"context"
	"github.com/gin-gonic/gin"
	"log"
	"service/component"
	"service/config"
	"service/model"
	"service/processor"
)

// RegisterRoutes 注册所有路由
func RegisterRoutes(r *gin.Engine, authProcessor *processor.AuthProcessor,
	codeSessionModel *model.CodeSessionRedisModel,
	userModel *model.UserRedisModel,
	pricingModel *model.AIPricingModel,
	taskModel *model.AITaskModel,
	apiConfigModel *model.AIAPIConfigModel,
	templateModel *model.TemplateModel,
	inspirationModel *model.InspirationAssetModel,
	templateCategoryModel *model.TemplateCategoryModel,
	templateSquareConfigModel *model.TemplateSquareConfigModel,
	templateUnlockModel *model.TemplateUnlockModel,
	templateLikeModel *model.TemplateLikeModel,
	templateCommentModel *model.TemplateCommentModel,
	templateShareModel *model.TemplateShareModel,
	userProfileModel *model.UserProfileModel,
	stoneRecordModel *model.StoneRecordModel,
	userOrderModel *model.UserOrderModel,
	inviteRelationModel *model.InviteRelationModel,
	userInviteCodeModel *model.UserInviteCodeModel,
	featuredCaseGroupModel *model.FeaturedCaseGroupModel,
	utilityToolModel *model.UtilityToolModel,
	aiToolModel *model.AIToolModel,
	rechargeConfigModel *model.RechargeConfigModel,
	membershipPlanModel *model.MembershipPlanModel,
	userMembershipModel *model.UserMembershipModel,
	supportTicketModel *model.SupportTicketModel,
	customerServiceModel *model.CustomerServiceModel,
	videoTaskModel *model.AIVideoTaskModel,
	certificationApplicationModel *model.CertificationApplicationModel,
	designerReviewModel *model.DesignerReviewModel,
	designerFollowModel *model.DesignerFollowModel,
	buildingCodeDocumentModel *model.BuildingCodeDocumentModel,
	contentAuditModel *model.AIContentAuditModel,
	buildingCodeChatModel *model.BuildingCodeChatModel,
	cfg *config.Config) func() {
	reportContext, stopReports := context.WithCancel(context.Background())
	// 管理端操作审计写入器（在下方管理路由块内初始化，停止时冲刷）
	var auditWriter *model.AuditWriter
	// 虚拟支付补偿任务：随服务关闭先停，避免 tick 与 DB/Redis 关闭赛跑
	var virtualPayCompensator *VirtualPayCompensator
	reportModule, reportErr := NewReportModule(reportContext, component.GetDB(), cfg)
	if reportErr != nil {
		log.Printf("[report] error module=disabled reason=readiness_check_failed")
	}
	if reportModule != nil {
		go reportModule.Run(reportContext)
	}
	// 健康检查
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"code": 0,
			"msg":  "ok",
		})
	})

	// API v1路由组
	v1 := r.Group("/api/v1")
	{
		// 用户DB模型（共用）
		userDBModel := model.NewUserModel(component.GetDB())
		trackingService := RegisterTrackingRoutes(v1, userDBModel, codeSessionModel)
		userFavoriteModel := model.NewUserFavoriteModel(component.GetDB())
		_ = userFavoriteModel.InitTable()
		setSharedTemplateSquareConfigModel(templateSquareConfigModel)

		// 小程序API
		miniprogram := v1.Group("/miniprogram")
		RegisterMiniprogramRoutes(miniprogram, authProcessor, codeSessionModel, userModel, stoneRecordModel, inviteRelationModel, userDBModel, userInviteCodeModel, userProfileModel, aiToolModel, templateSquareConfigModel)
		RegisterAIPublicRoutes(miniprogram, pricingModel)
		// 签到接口（简化token认证）
		checkinModel := model.NewCheckinModel(component.GetDB())
		RegisterCheckinRoutes(miniprogram, codeSessionModel, checkinModel, userModel, stoneRecordModel)
		// AI接口（需要token验证；封禁账号持旧 token 也一律拦截）
		miniprogramAI := miniprogram.Group("")
		miniprogramAI.Use(TokenAuthRequired(codeSessionModel))
		miniprogramAI.Use(MiniprogramBannedBlock(userDBModel))
		RegisterReportRoutes(miniprogram, miniprogramAI, v1, reportModule)
		RegisterAIRoutes(miniprogramAI, codeSessionModel, userModel, pricingModel, taskModel, stoneRecordModel, userOrderModel, inviteRelationModel, aiToolModel, templateModel, templateUnlockModel)
		RegisterRuralVillaDesignRoutes(miniprogramAI, codeSessionModel, userModel, pricingModel, taskModel, stoneRecordModel, userOrderModel, inviteRelationModel)
		RegisterAllroundDesignRoutes(miniprogramAI, codeSessionModel, userModel, pricingModel, taskModel, stoneRecordModel, userOrderModel, inviteRelationModel)
		RegisterBuildingReplacementRoutes(miniprogramAI, codeSessionModel, userModel, pricingModel, taskModel, stoneRecordModel, userOrderModel, inviteRelationModel)
		RegisterParentChildDesignRoutes(miniprogramAI, codeSessionModel, userModel, pricingModel, taskModel, stoneRecordModel, userOrderModel, inviteRelationModel)
		// AI造价接口（需要token验证）
		RegisterAICostRoutes(miniprogramAI, codeSessionModel, userModel, pricingModel, taskModel, stoneRecordModel, userOrderModel, inviteRelationModel, cfg)
		// AI任务信息接口（需要token验证）
		RegisterAITaskInfoRoutes(miniprogramAI, codeSessionModel, taskModel)
		// AI流式聊天接口（需要token验证）
		RegisterAIChatStreamRoute(miniprogramAI, codeSessionModel, userModel, pricingModel, apiConfigModel, cfg)
		// AI意图识别流式接口（需要token验证）
		RegisterAIIntentStreamRoute(miniprogramAI, codeSessionModel, cfg)
		// AI提示词优化接口（需要token验证+会员）
		RegisterPromptOptimizeRoute(miniprogramAI, codeSessionModel, userMembershipModel, cfg)
		RegisterBuildingCodeRoutes(miniprogramAI, buildingCodeDocumentModel, buildingCodeChatModel, userModel, pricingModel, cfg)
		RegisterCustomerServiceRoutes(miniprogramAI, customerServiceModel)
		// 任务状态轮询接口（需要token验证）
		RegisterTaskStatusRoute(miniprogramAI, codeSessionModel, userModel, pricingModel, taskModel, videoTaskModel, stoneRecordModel, aiToolModel, cfg)
		// AI 生成视频：发起、轮询、查询（结果存自家 OSS）
		RegisterAIVideoRoutes(miniprogramAI, codeSessionModel, userModel, pricingModel, stoneRecordModel, videoTaskModel, supportTicketModel, cfg)
		// 微信支付：JSAPI 预支付（需 token）、提现（需 token）、支付结果回调（v1 组下无需 token，含认证费回调）
		RegisterWechatPayRoutes(miniprogramAI, v1, cfg, userOrderModel, stoneRecordModel, userModel, userDBModel, certificationApplicationModel, membershipPlanModel, userMembershipModel)
		// 虚拟支付：小程序端 prepare 下单（需 token），confirm/补偿/推送由后续阶段补齐
		xpayMessageLogModel := model.NewXPayMessageLogModel(component.GetDB())
		sessionKeyModel := model.NewSessionKeyRedisModel(component.GetRedis())
		wechatCoinCalibrationModel := model.NewWechatCoinCalibrationModel(component.GetDB())
		pricingConfigModel := model.NewPricingConfigModel(component.GetDB())
		wechatOrderEvidenceModel := model.NewWechatOrderEvidenceModel(component.GetDB())
		virtualPayDeps := VirtualPayDeps{
			UserDBModel:                userDBModel,
			UserOrderModel:             userOrderModel,
			UserModel:                  userModel,
			StoneRecordModel:           stoneRecordModel,
			MembershipPlanModel:        membershipPlanModel,
			UserMembershipModel:        userMembershipModel,
			CertificationModel:         certificationApplicationModel,
			XPayMessageLogModel:        xpayMessageLogModel,
			SessionKeyModel:            sessionKeyModel,
			WechatCoinCalibrationModel: wechatCoinCalibrationModel,
			SupportTicketModel:         supportTicketModel,
			PricingConfigModel:         pricingConfigModel,
			WechatOrderEvidenceModel:   wechatOrderEvidenceModel,
		}
		if reportModule != nil {
			virtualPayDeps.ReportService = reportModule.Service
		}
		RegisterVirtualPayRoutes(miniprogramAI, cfg, userOrderModel, virtualPayDeps)
		// 启动补偿任务：每 60s 扫描卡 pending 的虚拟支付订单，主动推进发货
		virtualPayCompensator = NewVirtualPayCompensator(cfg, virtualPayDeps)
		virtualPayCompensator.Start()
		// 微信消息推送统一入口（v1 组下无需 token；验签解密 + 幂等去重 + 业务分发）
		RegisterWXMsgRoutes(v1, cfg, virtualPayDeps)
		// 认证申请（个人设计师/企业）：提交时支付认证费，支付成功后工单进入管理员审核
		RegisterCertificationRoutes(miniprogramAI, codeSessionModel, userDBModel, certificationApplicationModel, cfg, userOrderModel)
		// 用户数据接口（简化token认证，不需要签名参数）
		RegisterUserDataRoutes(miniprogram, codeSessionModel, userModel, pricingModel, taskModel, videoTaskModel, stoneRecordModel, userOrderModel, inviteRelationModel, userDBModel, templateModel, userProfileModel, userInviteCodeModel, certificationApplicationModel, designerReviewModel, designerFollowModel, aiToolModel, cfg)
		// 模板接口（公开接口，部分需要token验证）
		RegisterTemplateRoutes(miniprogram, templateModel, templateCategoryModel, templateSquareConfigModel, templateUnlockModel, templateLikeModel, templateCommentModel, templateShareModel, codeSessionModel, userModel, pricingModel, stoneRecordModel, userOrderModel, userProfileModel, userMembershipModel, userDBModel, featuredCaseGroupModel, taskModel, aiToolModel)
		RegisterInspirationRoutes(miniprogram, inspirationModel)
		RegisterFavoriteRoutes(miniprogram, codeSessionModel, userFavoriteModel, templateLikeModel, templateModel, inspirationModel, aiToolModel, userProfileModel, userDBModel, trackingService)
		// 用户信息修改接口
		RegisterUserProfileRoutes(miniprogram, codeSessionModel, userDBModel, userProfileModel)
		// 充值配置接口（公开接口）
		RegisterRechargeConfigRoutes(miniprogram, rechargeConfigModel, membershipPlanModel, pricingConfigModel, cfg)
		RegisterMembershipRoutes(miniprogram, codeSessionModel, membershipPlanModel, userMembershipModel, userOrderModel)
		RegisterAIToolRoutes(miniprogram, aiToolModel)
		RegisterPublicCustomerServiceRoutes(miniprogram, customerServiceModel)

		// 管理后台API
		management := v1.Group("/management")
		// 登录接口不需要认证
		RegisterManagementRoutes(management, authProcessor, userDBModel, userModel, userProfileModel, stoneRecordModel)

		// 需要认证的管理接口
		managementAuth := management.Group("")
		managementAuth.Use(AuthRequired, ManagementAccessRequired(userDBModel))

		// 操作审计（阶段 A）：记录所有写操作；建表 fail-soft（启动迁移白名单也会建）
		managementAuditModel := model.NewManagementAuditModel(userDBModel.DB)
		if err := managementAuditModel.InitTable(); err != nil {
			log.Printf("[审计] 初始化操作审计表失败（审计写入将丢失，请人工核查）: %v", err)
		}
		auditWriter = model.NewAuditWriter(managementAuditModel, 256)
		managementAuth.Use(ManagementAuditRecorder(auditWriter))
		RegisterManagementAuditRoutes(managementAuth, managementAuditModel)
		RegisterCRMManagementRoutes(managementAuth, userDBModel)
		RegisterDashboardManagementRoutes(managementAuth, userDBModel, customerServiceModel)
		// AI 定价版本表（调价留痕）：建表 fail-soft，失败只影响留痕不影响调价主流程
		pricingVersionModel := model.NewAIPricingVersionModel(userDBModel.DB)
		if err := pricingVersionModel.InitTable(); err != nil {
			log.Printf("[AI运营] 初始化定价版本表失败（调价将不留痕，请人工核查）: %v", err)
		}
		// AI 告警设置表：建表 fail-soft，失败则告警用默认阈值
		aiOpsSettingsModel := model.NewAIOpsSettingsModel(userDBModel.DB)
		if err := aiOpsSettingsModel.InitTable(); err != nil {
			log.Printf("[AI运营] 初始化告警设置表失败（告警将使用默认阈值）: %v", err)
		}
		RegisterAIManagementRoutes(managementAuth, pricingModel, apiConfigModel, pricingVersionModel)
		RegisterAIOverviewManagementRoutes(managementAuth, taskModel, videoTaskModel, pricingModel, pricingVersionModel, aiToolModel, supportTicketModel, aiOpsSettingsModel)
		// 内容抽检（4a）：模型在 main.go 初始化并注入 processor；nil 安全（建表失败时接口返回空）
		RegisterAIContentAuditManagementRoutes(managementAuth, contentAuditModel)
		RegisterAITaskManagementRoutes(managementAuth, pricingModel, taskModel, videoTaskModel, supportTicketModel)
		RegisterUserWorkbenchManagementRoutes(managementAuth, userDBModel, userModel, userProfileModel, stoneRecordModel, userOrderModel, userMembershipModel, taskModel, videoTaskModel, pricingModel)
		RegisterOrderManagementRoutes(managementAuth, userOrderModel, userDBModel, certificationApplicationModel, userMembershipModel, supportTicketModel, wechatCoinCalibrationModel, wechatOrderEvidenceModel)
		RegisterPricingManagementRoutes(managementAuth, pricingConfigModel, cfg)
		RegisterVirtualPayManagementRoutes(managementAuth, cfg, virtualPayDeps)
		RegisterDesignerManagementRoutes(managementAuth, userDBModel, templateModel, userProfileModel, certificationApplicationModel, stoneRecordModel, userOrderModel, designerReviewModel, designerFollowModel)
		RegisterBuildingCodeManagementRoutes(managementAuth, buildingCodeDocumentModel, cfg)
		RegisterDistributionManagementRoutes(managementAuth, userDBModel, inviteRelationModel, userInviteCodeModel, stoneRecordModel, userOrderModel)
		RegisterContentAnalyticsManagementRoutes(managementAuth, templateModel)
		RegisterRiskControlManagementRoutes(managementAuth, userDBModel)
		RegisterReportCenterManagementRoutes(managementAuth, userDBModel)
		RegisterTemplateManagementRoutes(managementAuth, templateModel, templateCategoryModel, templateSquareConfigModel, featuredCaseGroupModel)
		RegisterInspirationManagementRoutes(managementAuth, inspirationModel)
		RegisterUtilityToolManagementRoutes(managementAuth, utilityToolModel)
		RegisterAIToolManagementRoutes(managementAuth, aiToolModel, taskModel)
		RegisterRechargeConfigManagementRoutes(managementAuth, rechargeConfigModel)
		RegisterMembershipManagementRoutes(managementAuth, membershipPlanModel, userMembershipModel)
		RegisterMembershipOperationsManagementRoutes(managementAuth, userDBModel, userProfileModel, userMembershipModel, userOrderModel)
		// 账号合并向导：只读预检（执行走 DBA 定制流程，不在管理端开放）
		RegisterUserMergeManagementRoutes(managementAuth, userDBModel)
		RegisterSupportTicketManagementRoutes(managementAuth, supportTicketModel)
		RegisterCustomerLeadManagementRoutes(managementAuth, customerServiceModel)
		// 认证工单管理：列表、详情、审核（通过/拒绝），通过后更新用户 can_withdraw
		RegisterCertificationManagementRoutes(managementAuth, certificationApplicationModel, userDBModel, userProfileModel)

		// 小程序实用工具接口（公开接口）
		RegisterUtilityToolRoutes(miniprogram, utilityToolModel)
	}
	return func() {
		virtualPayCompensator.Stop()
		stopReports()
		auditWriter.Stop()
	}
}
