package route

// 管理端请求结构体。
//
// bool / 数字字段统一使用指针：Gin 的 binding:"required" 会把 false、0 当作"缺失"，
// 直接写在值类型上会让"取消精选""余额设为 0"这类合法请求必然返回 400。
// 指针让 required 只检查"字段是否出现"，同时仍能拒绝缺失字段，避免把缺省当成 false/0 而静默生效。

// setFeaturedRequest 设置/取消模板精选。
type setFeaturedRequest struct {
	IsFeatured *bool `json:"is_featured" binding:"required"`
}

// setStonesRequest 直接设置用户灵石余额（绝对值）。0 是合法余额。
type setStonesRequest struct {
	Stones *int64 `json:"stones" binding:"required,min=0"` // 新的灵石余额
	Remark string `json:"remark"`                          // 备注（可选）
}
