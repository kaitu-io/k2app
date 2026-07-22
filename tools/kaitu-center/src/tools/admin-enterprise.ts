/**
 * Enterprise router (multi-slot / multi-line) operator tools.
 */

import { z } from 'zod'
import { defineApiTool, type ToolRegistration } from '../tool-factory.js'

export const enterpriseTools: ToolRegistration[] = [
  defineApiTool({
    name: 'list_enterprise_customers',
    description: 'List enterprise router customers (company, contact, status, account)',
    group: 'enterprise',
    method: 'GET',
    params: { page: z.number().optional(), pageSize: z.number().optional() },
    path: () => `/app/enterprise/customers`,
  }),
  defineApiTool({
    name: 'create_enterprise_customer',
    description: 'Create an enterprise customer bound to an existing user account',
    group: 'enterprise.write',
    method: 'POST',
    params: { company: z.string(), contact: z.string().optional(), userId: z.number() },
    path: () => `/app/enterprise/customers`,
  }),
  defineApiTool({
    name: 'list_enterprise_lines',
    description: 'List lines of one enterprise customer (node, country, lineNo, status)',
    group: 'enterprise',
    method: 'GET',
    params: { customerId: z.number() },
    path: (p) => `/app/enterprise/customers/${p.customerId}/lines`,
  }),
  defineApiTool({
    name: 'create_enterprise_line',
    description: 'Attach a private node as an enterprise line (countryCode alpha-2 lowercase, lineNo>=1)',
    group: 'enterprise.write',
    method: 'POST',
    params: {
      customerId: z.number(),
      nodeId: z.number(),
      countryCode: z.string().regex(/^[a-z]{2}$/),
      lineNo: z.number().min(1),
    },
    path: () => `/app/enterprise/lines`,
  }),
  defineApiTool({
    name: 'list_enterprise_bindings',
    description: 'List router slot bindings by gateway deviceId or customerId',
    group: 'enterprise',
    method: 'GET',
    params: { deviceId: z.number().optional(), customerId: z.number().optional() },
    path: () => `/app/enterprise/bindings`,
  }),
  defineApiTool({
    name: 'bind_enterprise_slot',
    description: 'Bind (upsert) one router slot (1..8) to an enterprise line. Takes effect on next subs refresh.',
    group: 'enterprise.write',
    method: 'PUT',
    params: { gatewayDeviceId: z.number(), slot: z.number().min(1).max(8), lineId: z.number() },
    path: () => `/app/enterprise/bindings`,
  }),
  defineApiTool({
    name: 'unbind_enterprise_slot',
    description: 'Remove a slot binding by binding id (slot goes fail-closed/disabled on next refresh)',
    group: 'enterprise.write',
    method: 'DELETE',
    params: { bindingId: z.number() },
    path: (p) => `/app/enterprise/bindings/${p.bindingId}`,
  }),
]
