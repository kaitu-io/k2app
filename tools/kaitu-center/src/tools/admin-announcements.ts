/**
 * Admin announcement management tools.
 */

import { z } from 'zod'
import { defineApiTool, type ToolRegistration } from '../tool-factory.js'

export const announcementTools: ToolRegistration[] = [
  defineApiTool({
    name: 'list_announcements',
    description: 'List all announcements (paginated, includes inactive/expired).',
    group: 'announcements',
    path: '/app/announcements',
  }),

  defineApiTool({
    name: 'create_announcement',
    description: 'Create a new announcement.',
    group: 'announcements.write',
    method: 'POST',
    params: {
      message: z.string().describe('Announcement text (max 500 chars)'),
      link_url: z.string().optional().describe('Optional click target URL'),
      link_text: z.string().optional().describe('Optional link display text'),
      open_mode: z.enum(['external', 'webview']).optional().describe('Link open mode: external (default) or webview'),
      auth_mode: z.enum(['none', 'ott']).optional().describe('Auth mode: none (default) or ott (auto-login via one-time token)'),
      priority: z.number().optional().describe('Display priority (higher = shown first, default 0)'),
      min_version: z.string().optional().describe('Minimum app version (inclusive, e.g. "0.4.2")'),
      max_version: z.string().optional().describe('Maximum app version (inclusive, e.g. "0.4.3")'),
      expires_at: z.number().optional().describe('Expiry Unix timestamp (0 = never)'),
      is_active: z.boolean().optional().describe('Activate immediately'),
    },
    path: '/app/announcements',
    mapBody: (p) => ({
      message: p.message,
      linkUrl: p.link_url,
      linkText: p.link_text,
      openMode: p.open_mode,
      authMode: p.auth_mode,
      priority: p.priority,
      minVersion: p.min_version,
      maxVersion: p.max_version,
      expiresAt: p.expires_at,
      isActive: p.is_active,
    }),
  }),

  defineApiTool({
    name: 'update_announcement',
    description: 'Update an existing announcement.',
    group: 'announcements.write',
    method: 'PUT',
    params: {
      id: z.number().describe('Announcement ID'),
      message: z.string().describe('Announcement text (max 500 chars)'),
      link_url: z.string().optional().describe('Click target URL'),
      link_text: z.string().optional().describe('Link display text'),
      open_mode: z.enum(['external', 'webview']).optional().describe('Link open mode'),
      auth_mode: z.enum(['none', 'ott']).optional().describe('Auth mode: none or ott'),
      priority: z.number().optional().describe('Display priority (higher = shown first, default 0)'),
      min_version: z.string().optional().describe('Minimum app version (inclusive, e.g. "0.4.2")'),
      max_version: z.string().optional().describe('Maximum app version (inclusive, e.g. "0.4.3")'),
      expires_at: z.number().optional().describe('Expiry Unix timestamp (0 = never)'),
    },
    path: (p) => `/app/announcements/${p.id}`,
    mapBody: (p) => ({
      message: p.message,
      linkUrl: p.link_url,
      linkText: p.link_text,
      openMode: p.open_mode,
      authMode: p.auth_mode,
      priority: p.priority,
      minVersion: p.min_version,
      maxVersion: p.max_version,
      expiresAt: p.expires_at,
    }),
  }),

  defineApiTool({
    name: 'delete_announcement',
    description: 'Soft-delete an announcement by ID.',
    group: 'announcements.write',
    method: 'DELETE',
    params: {
      id: z.number().describe('Announcement ID'),
    },
    path: (p) => `/app/announcements/${p.id}`,
  }),

  defineApiTool({
    name: 'activate_announcement',
    description: 'Activate an announcement.',
    group: 'announcements.write',
    method: 'POST',
    params: {
      id: z.number().describe('Announcement ID'),
    },
    path: (p) => `/app/announcements/${p.id}/activate`,
  }),
]
