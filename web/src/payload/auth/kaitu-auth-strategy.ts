/**
 * Kaitu 自定义认证策略
 *
 * 从现有系统的 JWT Cookie 中验证用户身份
 * 如果用户拥有 CMS 相关角色，则允许访问 Payload CMS
 */

import type { AuthStrategy } from 'payload'
import jwt from 'jsonwebtoken'

// 角色常量（与 Go 后端保持一致）
export const Roles = {
  User: 1 << 0,      // 1 - 普通用户
  CMSAdmin: 1 << 1,  // 2 - CMS 管理员
  CMSEditor: 1 << 2, // 4 - CMS 编辑
  Super: 1 << 3,     // 8 - 超级管理员
} as const

// JWT Claims 结构（与 Go 后端保持一致）
// 注意：字段名必须与 Go 后端 TokenClaims 的 json tag 完全一致
interface KaituTokenClaims {
  user_id: number       // 用户ID
  device_id: string     // 设备ID
  exp: number           // 过期时间
  type: string          // token类型
  token_issue_at: number // 签发时间
  roles: number         // 角色位掩码（新增字段，旧 token 可能为 undefined）
}

// 检查是否拥有指定角色
export function hasRole(roles: number, role: number): boolean {
  return (roles & role) !== 0
}

// 检查是否有 CMS 访问权限
export function hasCMSAccess(roles: number): boolean {
  return hasRole(roles, Roles.CMSAdmin) ||
         hasRole(roles, Roles.CMSEditor) ||
         hasRole(roles, Roles.Super)
}

// 将 Kaitu 角色映射到 Payload 角色
export function mapToPayloadRole(roles: number): 'admin' | 'editor' {
  if (hasRole(roles, Roles.CMSAdmin) || hasRole(roles, Roles.Super)) {
    return 'admin'
  }
  return 'editor'
}

/**
 * Kaitu 认证策略
 *
 * 读取现有系统的 access_token cookie，验证 JWT 并检查 CMS 角色
 */
export const kaituAuthStrategy: AuthStrategy = {
  name: 'kaitu',

  authenticate: async ({ payload, headers }) => {
    // 获取 JWT Secret
    const jwtSecret = process.env.JWT_SECRET
    if (!jwtSecret) {
      console.warn('[KaituAuth] JWT_SECRET not configured')
      return { user: null }
    }

    // 从 Cookie 中获取 access_token
    const cookieHeader = headers.get('cookie')
    if (!cookieHeader) {
      return { user: null }
    }

    // 解析 cookie
    const cookies = Object.fromEntries(
      cookieHeader.split(';').map(c => {
        const [key, ...val] = c.trim().split('=')
        return [key, val.join('=')]
      })
    )

    const accessToken = cookies['access_token']
    if (!accessToken) {
      return { user: null }
    }

    try {
      // 验证 JWT
      const claims = jwt.verify(accessToken, jwtSecret) as KaituTokenClaims

      // 获取角色（旧 token 可能没有 roles 字段，默认为 0）
      const userRoles = claims.roles || 0

      // 检查是否有 CMS 访问权限
      if (!hasCMSAccess(userRoles)) {
        console.log(`[KaituAuth] User ${claims.user_id} has no CMS access (roles: ${userRoles})`)
        return { user: null }
      }

      // 确定 Payload 角色
      const payloadRole = mapToPayloadRole(userRoles)
      const kaituUserId = claims.user_id.toString()

      // 查找或创建 Payload 用户
      // 使用 kaituUserId 字段关联现有系统用户
      const existingUsers = await payload.find({
        collection: 'users',
        where: {
          kaituUserId: { equals: kaituUserId },
        },
        limit: 1,
      })

      let user = existingUsers.docs[0]

      if (!user) {
        // 创建新的 Payload 用户
        console.log(`[KaituAuth] Creating new Payload user for Kaitu user ${kaituUserId}`)
        user = await payload.create({
          collection: 'users',
          data: {
            email: `kaitu-${kaituUserId}@internal.kaitu.io`, // 内部占位邮箱
            password: crypto.randomUUID(), // 随机密码（不会使用）
            kaituUserId: kaituUserId,
            role: payloadRole,
          },
        })
      } else if (user.role !== payloadRole) {
        // 更新角色（如果后端角色发生变化）
        console.log(`[KaituAuth] Updating Payload user ${user.id} role from ${user.role} to ${payloadRole}`)
        user = await payload.update({
          collection: 'users',
          id: user.id,
          data: {
            role: payloadRole,
          },
        })
      }

      console.log(`[KaituAuth] User ${kaituUserId} authenticated as ${payloadRole}`)

      return {
        user: {
          collection: 'users',
          ...user,
        },
      }
    } catch (error) {
      if (error instanceof jwt.TokenExpiredError) {
        console.log('[KaituAuth] Token expired')
      } else if (error instanceof jwt.JsonWebTokenError) {
        console.log('[KaituAuth] Invalid token:', error.message)
      } else {
        console.error('[KaituAuth] Authentication error:', error)
      }
      return { user: null }
    }
  },
}
