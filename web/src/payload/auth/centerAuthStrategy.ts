import type { AuthStrategy } from 'payload'

type CenterLoginIdentify = {
  type: string
  value: string
}

type CenterUser = {
  uuid: string
  loginIdentifies?: CenterLoginIdentify[]
  roles: number
  isAdmin?: boolean
}

type CenterResponse = {
  code: number
  data?: CenterUser
}

const NON_USER_ROLES_MASK = 0xfffffffe

function parseCookie(header: string | null, name: string): string | null {
  if (!header) return null
  for (const part of header.split(';')) {
    const [k, ...rest] = part.trim().split('=')
    if (k === name) return decodeURIComponent(rest.join('='))
  }
  return null
}

function extractEmail(identifies: CenterLoginIdentify[] | undefined): string | null {
  if (!identifies) return null
  const match = identifies.find(i => i.type === 'email')
  return match?.value ?? null
}

export const centerAuthStrategy: AuthStrategy = {
  name: 'center-cookie',
  authenticate: async ({ headers, payload }) => {
    const token = parseCookie(headers.get('cookie'), 'access_token')
    if (!token) return { user: null }

    const centerUrl = process.env.CENTER_API_URL
    if (!centerUrl) {
      payload.logger.error('CENTER_API_URL not set')
      return { user: null }
    }

    let body: CenterResponse
    try {
      const res = await fetch(`${centerUrl}/api/user/info`, {
        headers: { Cookie: `access_token=${token}` },
      })
      if (!res.ok) return { user: null }
      body = (await res.json()) as CenterResponse
    } catch (e) {
      payload.logger.error({ msg: 'center auth fetch failed', err: e })
      return { user: null }
    }

    if (body.code !== 0 || !body.data) return { user: null }

    const isAdmin = body.data.isAdmin === true || (body.data.roles & NON_USER_ROLES_MASK) !== 0
    if (!isAdmin) return { user: null }

    const centerId = body.data.uuid
    const email = extractEmail(body.data.loginIdentifies)

    const existing = await payload.find({
      collection: 'admins',
      where: { centerId: { equals: centerId } },
      limit: 1,
    })

    let adminDoc = existing.docs[0]
    if (!adminDoc) {
      try {
        adminDoc = await payload.create({
          collection: 'admins',
          data: { email: email ?? '', centerId },
        })
      } catch (e) {
        // Likely unique-constraint race — another request created it first. Refetch.
        const refetch = await payload.find({
          collection: 'admins',
          where: { centerId: { equals: centerId } },
          limit: 1,
        })
        adminDoc = refetch.docs[0]
        if (!adminDoc) {
          payload.logger.error({ msg: 'admin upsert failed, no doc after refetch', err: e })
          return { user: null }
        }
      }
    }

    return {
      user: {
        ...adminDoc,
        collection: 'admins' as const,
      },
    }
  },
}
