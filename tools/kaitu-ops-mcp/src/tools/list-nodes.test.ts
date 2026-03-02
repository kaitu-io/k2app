import { describe, it, expect } from 'vitest'
import { filterNodes } from './list-nodes.js'

/**
 * Raw /app/nodes API response fixture with two nodes — jp-01 and sg-01.
 */
const rawNodesListResponse = {
  code: 0,
  data: {
    items: [
      {
        id: 1,
        name: 'jp-01',
        ipv4: '1.2.3.4',
        ipv6: '2001:db8::1',
        country: 'jp',
        region: 'tokyo',
        updatedAt: 1704067200,
        tunnels: [
          {
            id: 10,
            name: 'JP Tokyo K2V5',
            domain: 'jp.example.com',
            protocol: 'k2v5',
            port: 443,
            serverUrl: 'k2v5://jp.example.com:443?ech=AA&pin=sha256:BB',
          },
        ],
      },
      {
        id: 2,
        name: 'sg-01',
        ipv4: '5.6.7.8',
        ipv6: '2001:db8::2',
        country: 'sg',
        region: 'singapore',
        updatedAt: 1706745600,
        tunnels: [
          {
            id: 20,
            name: 'SG K2V4',
            domain: 'sg.example.com',
            protocol: 'k2v4',
            port: 8443,
            serverUrl: 'k2v5://sg.example.com:8443',
          },
          {
            id: 21,
            name: 'SG K2V5',
            domain: 'sg2.example.com',
            protocol: 'k2v5',
            port: 443,
            serverUrl: 'k2v5://sg2.example.com:443?ech=CC',
          },
        ],
      },
    ],
    pagination: {
      page: 1,
      pageSize: 500,
      total: 2,
    },
  },
}

describe('filterNodes', () => {
  it('extracts only safe fields from nodes list response — {name, ipv4, ipv6, country, region, tunnels}', () => {
    const result = filterNodes(rawNodesListResponse, {})

    expect(result).toHaveLength(2)
    const jp = result[0]
    expect(jp).toHaveProperty('name', 'jp-01')
    expect(jp).toHaveProperty('ipv4', '1.2.3.4')
    expect(jp).toHaveProperty('ipv6', '2001:db8::1')
    expect(jp).toHaveProperty('country', 'jp')
    expect(jp).toHaveProperty('region', 'tokyo')
    expect(jp).toHaveProperty('tunnels')
    // Ensure no extra fields leaked
    expect(Object.keys(jp ?? {})).toEqual(['name', 'ipv4', 'ipv6', 'country', 'region', 'tunnels'])
  })

  it('drops internal fields — id and updatedAt must be stripped from output', () => {
    const result = filterNodes(rawNodesListResponse, {})

    for (const node of result) {
      expect(node).not.toHaveProperty('id')
      expect(node).not.toHaveProperty('updatedAt')
    }
  })

  it('filter by country — country="jp" returns only Japanese nodes', () => {
    const result = filterNodes(rawNodesListResponse, { country: 'jp' })

    expect(result).toHaveLength(1)
    expect(result[0]).toHaveProperty('country', 'jp')
    expect(result[0]).toHaveProperty('name', 'jp-01')
  })

  it('filter by name — name="sg-01" returns only matching node', () => {
    const result = filterNodes(rawNodesListResponse, { name: 'sg-01' })

    expect(result).toHaveLength(1)
    expect(result[0]).toHaveProperty('name', 'sg-01')
  })

  it('empty result — no nodes match filter returns empty array', () => {
    const result = filterNodes(rawNodesListResponse, { country: 'us' })

    expect(result).toHaveLength(0)
    expect(Array.isArray(result)).toBe(true)
  })

  it('tunnel mapping — tunnels nested under each node with correct keys', () => {
    const result = filterNodes(rawNodesListResponse, {})

    const jp = result[0]
    expect(jp?.tunnels).toHaveLength(1)
    const tunnel = jp?.tunnels[0]
    expect(tunnel).toHaveProperty('name', 'JP Tokyo K2V5')
    expect(tunnel).toHaveProperty('country', 'jp')
    expect(tunnel).toHaveProperty('domain', 'jp.example.com')
    expect(tunnel).toHaveProperty('protocol', 'k2v5')
    expect(tunnel).toHaveProperty('port', 443)
    expect(tunnel).toHaveProperty('url', 'k2v5://jp.example.com:443?ech=AA&pin=sha256:BB')
    // id must be stripped
    expect(tunnel).not.toHaveProperty('id')
    // serverUrl must not appear (mapped to url)
    expect(tunnel).not.toHaveProperty('serverUrl')
    // tunnel keys are exactly the 6 expected
    expect(Object.keys(tunnel ?? {})).toEqual(['name', 'country', 'domain', 'protocol', 'port', 'url'])

    // sg-01 has 2 tunnels
    const sg = result[1]
    expect(sg?.tunnels).toHaveLength(2)
  })

  it('passes through meta field when present', () => {
    const responseWithMeta = {
      code: 0,
      data: {
        items: [
          {
            id: 1,
            name: 'jp-01',
            ipv4: '1.2.3.4',
            ipv6: '2001:db8::1',
            country: 'jp',
            region: 'tokyo',
            updatedAt: 1704067200,
            tunnels: [],
            meta: { arch: 'k2v5' },
          },
        ],
        pagination: { page: 1, pageSize: 500, total: 1 },
      },
    }

    const result = filterNodes(responseWithMeta, {})
    expect(result).toHaveLength(1)
    expect(result[0]?.meta).toEqual({ arch: 'k2v5' })
  })

  it('omits meta field when not present in raw response', () => {
    const result = filterNodes(rawNodesListResponse, {})
    expect(result[0]?.meta).toBeUndefined()
  })
})
