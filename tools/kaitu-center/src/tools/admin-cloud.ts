/**
 * Admin cloud instance and resource management tools.
 */

import { z } from 'zod'
import { defineApiTool, type ToolRegistration } from '../tool-factory.js'

export const cloudTools: ToolRegistration[] = [
  defineApiTool({
    name: 'list_cloud_instances',
    description: 'List cloud VPS instances with pagination.',
    group: 'cloud',
    params: {
      page: z.number().optional().describe('Page number'),
      page_size: z.number().optional().describe('Page size'),
    },
    path: '/app/cloud/instances',
    mapQuery: (p) => {
      const q: Record<string, string> = {}
      if (p.page !== undefined) q.page = String(p.page)
      if (p.page_size !== undefined) q.pageSize = String(p.page_size)
      return q
    },
  }),

  defineApiTool({
    name: 'get_cloud_instance',
    description: 'Get details for a single cloud instance.',
    group: 'cloud',
    params: {
      id: z.number().describe('Instance ID'),
    },
    path: (p) => `/app/cloud/instances/${p.id}`,
  }),

  defineApiTool({
    name: 'create_cloud_instance',
    description: 'Provision a new cloud VPS instance.',
    group: 'cloud.write',
    method: 'POST',
    params: {
      account_id: z.number().describe('Cloud account ID'),
      region: z.string().describe('Region identifier'),
      plan: z.string().describe('Instance plan/size'),
      image: z.string().optional().describe('OS image identifier'),
    },
    path: '/app/cloud/instances',
    mapBody: (p) => ({
      accountId: p.account_id,
      region: p.region,
      plan: p.plan,
      image: p.image,
    }),
  }),

  defineApiTool({
    name: 'delete_cloud_instance',
    description: 'Destroy a cloud instance by ID.',
    group: 'cloud.write',
    method: 'DELETE',
    params: {
      id: z.number().describe('Instance ID'),
    },
    path: (p) => `/app/cloud/instances/${p.id}`,
  }),

  defineApiTool({
    name: 'sync_cloud_instances',
    description: 'Sync cloud instance state from all providers.',
    group: 'cloud.write',
    method: 'POST',
    path: '/app/cloud/instances/sync',
  }),

  defineApiTool({
    name: 'change_ip_cloud_instance',
    description: 'Request an IP address change for a cloud instance.',
    group: 'cloud.write',
    method: 'POST',
    params: {
      id: z.number().describe('Instance ID'),
    },
    path: (p) => `/app/cloud/instances/${p.id}/change-ip`,
  }),

  defineApiTool({
    name: 'update_traffic_config',
    description: 'Update monthly traffic limit for a cloud instance.',
    group: 'cloud.write',
    method: 'PUT',
    params: {
      id: z.number().describe('Instance ID'),
      monthly_limit_gb: z.number().optional().describe('Monthly traffic limit in GB'),
    },
    path: (p) => `/app/cloud/instances/${p.id}/traffic-config`,
    mapBody: (p) => ({
      monthlyLimitGb: p.monthly_limit_gb,
    }),
  }),

  defineApiTool({
    name: 'list_cloud_accounts',
    description: 'List configured cloud provider accounts.',
    group: 'cloud',
    path: '/app/cloud/accounts',
  }),

  defineApiTool({
    name: 'list_cloud_regions',
    description: 'List available cloud regions across providers.',
    group: 'cloud',
    path: '/app/cloud/regions',
  }),

  defineApiTool({
    name: 'list_cloud_plans',
    description: 'List available cloud instance plans/sizes.',
    group: 'cloud',
    path: '/app/cloud/plans',
  }),

  defineApiTool({
    name: 'list_cloud_images',
    description: 'List available OS images for cloud instances.',
    group: 'cloud',
    path: '/app/cloud/images',
  }),
]
