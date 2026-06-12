package center

// PrivateNodeTrafficWarnMeta 专属线路流量预警邮件渲染数据
type PrivateNodeTrafficWarnMeta struct {
	Percent   int
	UsedGB    string
	TotalGB   string
	Region    string
	ResetDate string
}

// privateNodeTrafficWarnTemplate 专属线路本月流量达到 80%/95% 时的预警邮件
var privateNodeTrafficWarnTemplate = EmailTemplate[PrivateNodeTrafficWarnMeta]{
	Subject: "【开途专属线路】本月流量已用 {{.Percent}}%",
	Body: `您好：

您的开途专属线路（{{.Region}}）本月流量已使用 {{.Percent}}%（{{.UsedGB}} / {{.TotalGB}}）。

专属线路达到 100% 后，为避免产生超额费用，路由器将暂停新建连接，海外访问会中断，直至 {{.ResetDate}} 流量重置。

如需更多流量，可升级线路档位或加购第二条专属线路。

—— 开途团队`,
}
