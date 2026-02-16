// Package waymaker 提供 WayMaker/k2oc 协议支持
// 使用 certtool (GnuTLS) 签名证书，与 wgcenter 完全一致
// 所有资源文件 embed 到二进制中，运行时释放到临时目录
package waymaker

import _ "embed"

// Legacy CA 证书（RSA）- 用于 k2oc 协议客户端验证
//
//go:embed ca_cert.pem
var LegacyCACert []byte

// Legacy CA 私钥（RSA）- 用于证书签名
//
//go:embed ca_key.pem
var LegacyCAKey []byte

// 证书生成脚本 - 使用 certtool (GnuTLS) 签名
//
//go:embed generate-key4domain.sh
var GenerateScript []byte
