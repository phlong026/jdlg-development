仅供微信原生运行环境验证；本云环境未运行它，不能视为真机通过。
没有 wx.request、uploadFile、登录、生产域名或服务端写入；不会保存昵称正文。
打开 project.config.json 所在目录，用项目有权限的微信开发者工具在真机调试。
保持与发布包相同基础库，分别测试 Android/iOS，记录微信与基础库版本。
基线页与 main 一样只绑定 input，观测页额外记录 change/blur/nicknamereview。
比较：原生选昵称后立即点完成；选完先收起键盘；手动输入后提交；修改昵称后立即提交；审核通过/不通过/超时。
核查 submit.value.nickname 是否含最终字符串；不要拿 review.detail 当作昵称，它可能只有 pass/timeout。
console 日志含事件时间、结构、长度、是否等于 input 缓存，不含昵称正文。
审核先后顺序、表单是否自动等待审核、基础库差异必须以真机日志为准。
