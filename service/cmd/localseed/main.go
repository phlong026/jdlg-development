// localseed 向本机 Docker 里的 MySQL 写入虚构的联调数据：管理账号、小程序用户、订单、模板。
//
// 仅供本地联调。安全护栏：DSN 的主机不是 127.0.0.1 / localhost，或 ENV=production 时拒绝运行，
// 防止误把虚构数据写进线上库。
//
// 本地管理账号（仅本地、虚构）：
//
//	admin_local  平台管理员（platform_admin）
//	crm_local    仅客户管理（crm_only）
//
// 两个账号的口令均为常量 localPassword。
package main

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	mysqlDriver "github.com/go-sql-driver/mysql"

	"service/config"
	"service/function"
	"service/model"
)

const localPassword = "LocalAdmin!2026"

func main() {
	if err := run(); err != nil {
		log.Fatalf("localseed: %v", err)
	}
}

func run() error {
	if err := config.LoadLocalEnv(); err != nil {
		return fmt.Errorf("读取本地环境配置失败: %w", err)
	}
	if err := guardLocalOnly(os.Getenv("MYSQL_DSN"), os.Getenv("ENV")); err != nil {
		return err
	}
	db, err := sql.Open("mysql", os.Getenv("MYSQL_DSN"))
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		return fmt.Errorf("连接本地 MySQL 失败（是否已执行 dev.sh up / start？）: %w", err)
	}

	users := model.NewUserModel(db)
	adminID, err := ensureManagementUser(users, "admin_local")
	if err != nil {
		return err
	}
	crmID, err := ensureManagementUser(users, "crm_local")
	if err != nil {
		return err
	}
	for id, scope := range map[int64]string{adminID: "platform_admin", crmID: "crm_only"} {
		if _, err := db.Exec(`INSERT INTO management_access(user_id,scope,enabled,version,updated_by,updated_at)
			VALUES (?,?,TRUE,0,?,NOW(6))
			ON DUPLICATE KEY UPDATE scope=VALUES(scope), enabled=TRUE`, id, scope, adminID); err != nil {
			return fmt.Errorf("登记管理授权失败（是否已执行 dev.sh migrate？）: %w", err)
		}
	}

	userIDs, err := ensureMiniprogramUsers(users, 12)
	if err != nil {
		return err
	}
	if err := seedOrders(db, userIDs); err != nil {
		return err
	}
	if err := seedTemplates(db); err != nil {
		return err
	}
	if err := seedLeads(db, userIDs); err != nil {
		return err
	}
	log.Printf("完成：管理账号 admin_local / crm_local，小程序用户 %d 个，订单、模板与客服线索已写入", len(userIDs))
	return nil
}

// guardLocalOnly 只允许写入本机数据库。
func guardLocalOnly(dsn, env string) error {
	if strings.EqualFold(strings.TrimSpace(env), "production") {
		return errors.New("ENV=production，拒绝写入虚构数据")
	}
	// 空 DSN 会被驱动补成默认的 127.0.0.1:3306，可能写进本机上不相干的库，必须显式配置。
	if strings.TrimSpace(dsn) == "" {
		return errors.New("MYSQL_DSN 未设置，拒绝使用驱动默认值")
	}
	cfg, err := mysqlDriver.ParseDSN(dsn)
	if err != nil {
		return fmt.Errorf("无法解析 MYSQL_DSN: %w", err)
	}
	host := cfg.Addr
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	if host != "127.0.0.1" && host != "localhost" {
		return fmt.Errorf("数据库主机 %q 不是本机，拒绝写入虚构数据", host)
	}
	return nil
}

func ensureManagementUser(users *model.UserModel, name string) (int64, error) {
	if u, err := users.GetByUsernameAndType(name, "management"); err == nil && u != nil {
		return u.ID, nil
	}
	hash, err := function.HashManagementPassword(localPassword)
	if err != nil {
		return 0, err
	}
	// users.openid 有唯一索引，UserModel.Create 会把空值写成空字符串，第二个账号就会冲突，
	// 因此给每个账号一个唯一的占位 openid。
	u := &model.User{Username: name, Password: hash, UserType: "management", OpenID: "local_mgmt_" + name}
	if err := users.Create(u); err != nil {
		return 0, fmt.Errorf("创建管理账号 %s 失败: %w", name, err)
	}
	return u.ID, nil
}

func ensureMiniprogramUsers(users *model.UserModel, n int) ([]int64, error) {
	ids := make([]int64, 0, n)
	for i := 1; i <= n; i++ {
		name := fmt.Sprintf("wx_local_%02d", i)
		if u, err := users.GetByUsernameAndType(name, "miniprogram"); err == nil && u != nil {
			ids = append(ids, u.ID)
			continue
		}
		u := &model.User{Username: name, UserType: "miniprogram", OpenID: fmt.Sprintf("local_openid_%02d", i)}
		if err := users.Create(u); err != nil {
			return nil, fmt.Errorf("创建小程序用户 %s 失败: %w", name, err)
		}
		ids = append(ids, u.ID)
	}
	return ids, nil
}

// seedOrders 写入 118 笔订单。异常单（失败/待处理）先写入，随后按 id 顺序递增 created_at，
// 因此它们位于按时间倒序的最后一页：用来验证"仅看异常单"必须在全量数据上筛选。
func seedOrders(db *sql.DB, userIDs []int64) error {
	var existing int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_orders WHERE order_no LIKE 'LOCAL%'`).Scan(&existing); err != nil {
		return err
	}
	if existing > 0 {
		log.Printf("订单已存在（%d 笔），仅校正支付通道与时间", existing)
		return normalizeLocalOrders(db)
	}
	orders := model.NewUserOrderModel(db)
	type spec struct{ typ, category, status, channel, title string }
	var plan []spec
	for i := 0; i < 3; i++ {
		plan = append(plan, spec{"recharge", "recharge", "failed", "virtualpay", "灵石充值（失败）"})
		plan = append(plan, spec{"recharge", "recharge", "pending", "virtualpay", "灵石充值（待处理）"})
	}
	for i := 0; i < 110; i++ {
		if i%3 == 0 {
			plan = append(plan, spec{"consume", "ai", "success", "", "AI 生图"})
		} else {
			plan = append(plan, spec{"recharge", "recharge", "success", "virtualpay", "灵石充值"})
		}
	}
	plan = append(plan, spec{"recharge", "recharge", "refunding", "virtualpay", "灵石充值（退款中）"})
	plan = append(plan, spec{"recharge", "recharge", "refunded", "virtualpay", "灵石充值（已退款）"})

	for i, s := range plan {
		amount := int64(100 + (i%5)*100)
		if s.typ == "consume" {
			amount = -40
		}
		o := &model.UserOrder{
			UserID: userIDs[i%len(userIDs)], OrderNo: fmt.Sprintf("LOCAL%05d", i+1),
			Type: s.typ, OrderCategory: s.category, Amount: amount, Status: s.status,
			Title: s.title, Description: "localseed 虚构订单", PayChannel: s.channel,
		}
		if s.channel == "virtualpay" {
			o.PayMode = "short_series_coin"
			o.AmountFen = amount * 10
		}
		if err := orders.CreateDetailed(o); err != nil {
			return fmt.Errorf("写入订单 %s 失败: %w", o.OrderNo, err)
		}
	}
	return normalizeLocalOrders(db)
}

// normalizeLocalOrders 幂等地校正本地订单：
//   - CreateDetailed 不写 pay_channel / pay_mode，充值单默认成了"微信支付"，
//     这里改成虚拟支付，订单页的虚拟支付链接与退款入口才会出现；
//   - 让较晚写入的订单更"新"，保证按时间倒序时异常单在最后一页。
func normalizeLocalOrders(db *sql.DB) error {
	if _, err := db.Exec(`UPDATE user_orders SET pay_channel='virtualpay', pay_mode='short_series_coin', amount_fen=amount*10
		WHERE order_no LIKE 'LOCAL%' AND type='recharge'`); err != nil {
		return err
	}
	_, err := db.Exec(`UPDATE user_orders SET created_at = TIMESTAMPADD(MINUTE, id, '2026-09-01 00:00:00') WHERE order_no LIKE 'LOCAL%'`)
	return err
}

func seedTemplates(db *sql.DB) error {
	var existing int
	if err := db.QueryRow(`SELECT COUNT(*) FROM templates WHERE creator = 'localseed'`).Scan(&existing); err != nil {
		return err
	}
	if existing > 0 {
		log.Printf("模板已存在（%d 个），跳过", existing)
		return nil
	}
	templates := model.NewTemplateModel(db)
	for i, name := range []string{"新中式三层乡墅", "闽南红砖院落", "现代极简小筑", "田园民宿改造"} {
		t := &model.Template{
			Name: name, Category: "villa", Status: "published", IsFree: true,
			IsFeatured: i < 2, Creator: "localseed", Description: "localseed 虚构模板",
		}
		if err := templates.Create(t); err != nil {
			return fmt.Errorf("写入模板 %s 失败: %w", name, err)
		}
	}
	return nil
}

// seedLeads 写入虚构的客服线索：同一个用户可以有多条线索，也有未关联账号的匿名线索，
// 并覆盖各种状态 / 负责人组合，使各队列（待处理、未分配、高意向……）的数量互不相同，
// 这样"总览数量必须等于队列列表条数"的检查才有区分度。每次执行都会先清掉上一次种下的本地线索再重建，可重复执行。
func seedLeads(db *sql.DB, userIDs []int64) error {
	if len(userIDs) < 3 {
		return fmt.Errorf("至少需要 3 个小程序用户才能写入线索，当前 %d 个", len(userIDs))
	}
	// CreateLead 会把同一用户的开放线索合并，重复执行时按 id 更新状态会落空；
	// 先清掉上次种下的本地线索（仅 LOCAL-S 开头的会话号，且 guardLocalOnly 已保证只写本机库）再重建，保证幂等。
	if _, err := db.Exec(`DELETE FROM customer_lead_followups WHERE lead_id IN (SELECT id FROM customer_leads WHERE session_no LIKE 'LOCAL-S%')`); err != nil {
		return fmt.Errorf("清理旧的本地线索跟进记录失败: %w", err)
	}
	if _, err := db.Exec(`DELETE FROM customer_leads WHERE session_no LIKE 'LOCAL-S%'`); err != nil {
		return fmt.Errorf("清理旧的本地线索失败: %w", err)
	}
	leads := model.NewCustomerServiceModel(db)
	type spec struct {
		input      model.CustomerLeadCreateInput
		status     string // 为空表示保持"新线索"
		assignedTo string
	}
	specs := []spec{
		{input: model.CustomerLeadCreateInput{UserID: userIDs[0], Name: "林先生", Phone: "13800000001", SessionNo: "LOCAL-S1", DemandSummary: "两层闽南红砖自建房，宅基地 12 米宽 15 米深，想要三个卧室，预算 40 万，希望尽快电话沟通"}},
		{input: model.CustomerLeadCreateInput{UserID: userIDs[0], Name: "林先生", Phone: "13800000001", SessionNo: "LOCAL-S2", DemandSummary: "想再看看三层方案，还没想好"}, status: "contacted"},
		{input: model.CustomerLeadCreateInput{UserID: userIDs[1], Name: "陈女士", Wechat: "chen_local", SessionNo: "LOCAL-S3", DemandSummary: "新中式庭院，三层，留微信方便联系"}, status: "contacted", assignedTo: "sales_a"},
		{input: model.CustomerLeadCreateInput{UserID: userIDs[2], Name: "黄先生", Phone: "13800000003", SessionNo: "LOCAL-S4", DemandSummary: "现代极简别墅，想了解造价"}, status: "converted", assignedTo: "sales_a"},
		{input: model.CustomerLeadCreateInput{UserID: 0, Name: "匿名访客", SessionNo: "LOCAL-S5", DemandSummary: "随便问问"}, status: "invalid"},
		{input: model.CustomerLeadCreateInput{UserID: userIDs[1], Name: "陈女士", Phone: "13800000002", SessionNo: "LOCAL-S6", DemandSummary: "两层闽南红砖自建房，宅基地 10 米宽 14 米深，预算 35 万，今天想电话沟通"}, status: "converted", assignedTo: "sales_b"},
		// 仍待跟进的高意向线索：保证「高意向」队列非空，队列数量检查才有区分度
		{input: model.CustomerLeadCreateInput{UserID: userIDs[3], Name: "吴先生", Phone: "13800000004", SessionNo: "LOCAL-S7", DemandSummary: "两层闽南红砖自建房，宅基地 11 米宽 16 米深，预算 45 万，今天想电话沟通"}},
	}
	for i := range specs {
		input := specs[i].input
		input.Source = "ai_customer_service"
		id, err := leads.CreateLead(&input)
		if err != nil {
			return fmt.Errorf("写入线索 %s 失败（是否已执行过一次 start 让表自动创建？）: %w", input.SessionNo, err)
		}
		if specs[i].status == "" {
			continue
		}
		action := model.CustomerLeadActionInput{Status: specs[i].status, AssignedTo: specs[i].assignedTo, Remark: "localseed 虚构跟进"}
		if err := leads.ApplyLeadAction(id, action); err != nil {
			return fmt.Errorf("更新线索 %s 状态失败: %w", input.SessionNo, err)
		}
	}
	return nil
}
