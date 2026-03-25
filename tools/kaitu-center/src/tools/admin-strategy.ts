/**
 * Admin strategy rule management tools.
 */

import { z } from 'zod'
import { defineApiTool, type ToolRegistration } from '../tool-factory.js'

export const strategyTools: ToolRegistration[] = [
  defineApiTool({
    name: 'list_strategy_rules',
    description: 'List all strategy rule versions.',
    group: 'strategy',
    path: '/app/strategy/rules',
  }),

  defineApiTool({
    name: 'get_strategy_rule',
    description: 'Get a strategy rule by version string.',
    group: 'strategy',
    params: {
      version: z.string().describe('Rule version identifier'),
    },
    path: (p) => `/app/strategy/rules/${p.version}`,
  }),

  defineApiTool({
    name: 'create_strategy_rule',
    description: 'Create a new strategy rule version with JSON config.',
    group: 'strategy.write',
    method: 'POST',
    params: {
      rules: z.unknown().describe('Strategy rule JSON configuration'),
    },
    path: '/app/strategy/rules',
  }),

  defineApiTool({
    name: 'activate_strategy_rule',
    description: 'Activate a strategy rule version (makes it the live version).',
    group: 'strategy.write',
    method: 'PUT',
    params: {
      version: z.string().describe('Rule version identifier'),
    },
    path: (p) => `/app/strategy/rules/${p.version}/activate`,
  }),

  defineApiTool({
    name: 'delete_strategy_rule',
    description: 'Delete a strategy rule version.',
    group: 'strategy.write',
    method: 'DELETE',
    params: {
      version: z.string().describe('Rule version identifier'),
    },
    path: (p) => `/app/strategy/rules/${p.version}`,
  }),
]
