import { describe, it, expect } from 'vitest'
import { filterNodes } from './list-nodes.js'

/**
 * Raw batch-matrix API response fixture with two nodes — jp-01 and sg-01.
 */
const rawBatchMatrixResponse = {
  code: 0,
  data: {
    nodes: [
      {
        id: 1,
        name: 'jp-01',
        ipv4: '1.2.3.4',
        ipv6: '2001:db8::1',
        country: 'jp',
        region: 'tokyo',
        status: 'online',
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-02T00:00:00Z',
        tunnels: [
          {
            id: 10,
            domain: 'jp.example.com',
            protocol: 'k2v5',
            port: 443,
            server_url: 'https://jp.example.com',
          },
        ],
        batch_script_results: {
          cpu: '10%',
          memory: '512MB',
          secret_data: 'sensitive-value',
        },
      },
      {
        id: 2,
        name: 'sg-01',
        ipv4: '5.6.7.8',
        ipv6: '2001:db8::2',
        country: 'sg',
        region: 'singapore',
        status: 'offline',
        created_at: '2024-02-01T00:00:00Z',
        updated_at: '2024-02-02T00:00:00Z',
        tunnels: [
          {
            id: 20,
            domain: 'sg.example.com',
            protocol: 'k2v4',
            port: 8443,
            server_url: 'https://sg.example.com',
          },
          {
            id: 21,
            domain: 'sg2.example.com',
            protocol: 'k2v5',
            port: 443,
            server_url: 'https://sg2.example.com',
          },
        ],
        batch_script_results: {},
      },
    ],
  },
}

describe('filterNodes', () => {
  it('test_list_nodes_field_extraction — raw batch-matrix response filtered to {name, ipv4, ipv6, country, region, tunnels}', () => {
    const result = filterNodes(rawBatchMatrixResponse, {})

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

  it('test_list_nodes_drops_script_results — batch_script_results must be stripped from output', () => {
    const result = filterNodes(rawBatchMatrixResponse, {})

    for (const node of result) {
      expect(node).not.toHaveProperty('batch_script_results')
      expect(node).not.toHaveProperty('id')
      expect(node).not.toHaveProperty('status')
      expect(node).not.toHaveProperty('created_at')
      expect(node).not.toHaveProperty('updated_at')
    }
  })

  it('test_list_nodes_filter_by_country — country="jp" returns only Japanese nodes', () => {
    const result = filterNodes(rawBatchMatrixResponse, { country: 'jp' })

    expect(result).toHaveLength(1)
    expect(result[0]).toHaveProperty('country', 'jp')
    expect(result[0]).toHaveProperty('name', 'jp-01')
  })

  it('test_list_nodes_filter_by_name — name="sg-01" returns only matching node', () => {
    const result = filterNodes(rawBatchMatrixResponse, { name: 'sg-01' })

    expect(result).toHaveLength(1)
    expect(result[0]).toHaveProperty('name', 'sg-01')
  })

  it('test_list_nodes_empty_result — no nodes match filter returns empty array', () => {
    const result = filterNodes(rawBatchMatrixResponse, { country: 'us' })

    expect(result).toHaveLength(0)
    expect(Array.isArray(result)).toBe(true)
  })

  it('test_list_nodes_tunnel_mapping — tunnels nested under each node with camelCase keys', () => {
    const result = filterNodes(rawBatchMatrixResponse, {})

    const jp = result[0]
    expect(jp?.tunnels).toHaveLength(1)
    const tunnel = jp?.tunnels[0]
    // camelCase keys
    expect(tunnel).toHaveProperty('domain', 'jp.example.com')
    expect(tunnel).toHaveProperty('protocol', 'k2v5')
    expect(tunnel).toHaveProperty('port', 443)
    expect(tunnel).toHaveProperty('serverUrl', 'https://jp.example.com')
    // snake_case key must NOT appear
    expect(tunnel).not.toHaveProperty('server_url')
    // id must be stripped
    expect(tunnel).not.toHaveProperty('id')
    // tunnel keys are exactly the 4 expected
    expect(Object.keys(tunnel ?? {})).toEqual(['domain', 'protocol', 'port', 'serverUrl'])

    // sg-01 has 2 tunnels
    const sg = result[1]
    expect(sg?.tunnels).toHaveLength(2)
  })

  it('passes through meta field when present', () => {
    const responseWithMeta = {
      code: 0,
      data: {
        nodes: [
          {
            id: 1,
            name: 'jp-01',
            ipv4: '1.2.3.4',
            ipv6: '2001:db8::1',
            country: 'jp',
            region: 'tokyo',
            status: 'online',
            created_at: '2024-01-01T00:00:00Z',
            updated_at: '2024-01-02T00:00:00Z',
            tunnels: [],
            batch_script_results: {},
            meta: { arch: 'k2v5' },
          },
        ],
      },
    }

    const result = filterNodes(responseWithMeta, {})
    expect(result).toHaveLength(1)
    expect(result[0]?.meta).toEqual({ arch: 'k2v5' })
  })

  it('omits meta field when not present in raw response', () => {
    const result = filterNodes(rawBatchMatrixResponse, {})
    expect(result[0]?.meta).toBeUndefined()
  })
})
