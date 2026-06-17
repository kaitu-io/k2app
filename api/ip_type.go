package center

// NormalizeIPType 把任意输入收敛到封闭枚举(C1)。精确匹配三值之一,否则 unknown。
func NormalizeIPType(s string) string {
	switch s {
	case IPTypeResidential, IPTypeNonResidential, IPTypeUnknown:
		return s
	default:
		return IPTypeUnknown
	}
}

// ProtocolDisplay 是 k2v5→k2s 的唯一显示映射点(C2)。wire/DB 值不变。
func ProtocolDisplay(p TunnelProtocol) string {
	if p == TunnelProtocolK2V5 {
		return "k2s"
	}
	return string(p)
}
