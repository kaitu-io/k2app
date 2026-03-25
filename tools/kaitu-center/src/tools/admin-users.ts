/**
 * User management tools (factory declarations).
 *
 * Migrated: lookup_user, list_user_devices.
 * New: add_user_membership, update_user_email, set_user_roles,
 *      generate_access_key, revoke_access_key, list_user_members,
 *      add_user_member, remove_user_member, update_user_retailer_status,
 *      hard_delete_users.
 */

import { z } from 'zod'
import { defineApiTool, type ToolRegistration } from '../tool-factory.js'

export const userTools: ToolRegistration[] = [
  // --- Migrated ---

  defineApiTool({
    name: 'lookup_user',
    description: 'Look up a user by email or UUID. Provide one of email or uuid.',
    group: 'users',
    path: (p) => (p.uuid ? `/app/users/${p.uuid}` : '/app/users'),
    params: {
      email: z.string().optional().describe('User email to search for'),
      uuid: z.string().optional().describe('User UUID for direct lookup'),
    },
    mapQuery: (p) => {
      if (p.uuid) return {}
      const q: Record<string, string> = {}
      if (p.email) q.email = String(p.email)
      return q
    },
  }),

  defineApiTool({
    name: 'list_user_devices',
    description: 'List all devices registered to a user. Returns UDID, platform, app version, last seen time.',
    group: 'users',
    path: (p) => `/app/users/${p.uuid}/devices`,
    params: {
      uuid: z.string().describe('User UUID'),
    },
  }),

  // --- New ---

  defineApiTool({
    name: 'add_user_membership',
    description: 'Add membership days to a user account.',
    group: 'users.write',
    method: 'POST',
    path: (p) => `/app/users/${p.uuid}/membership`,
    params: {
      uuid: z.string().describe('User UUID'),
      months: z.number().describe('Number of months to add'),
      reason: z.string().optional().describe('Reason for adding membership'),
    },
  }),

  defineApiTool({
    name: 'update_user_email',
    description: 'Update a user email address.',
    group: 'users.write',
    method: 'PUT',
    path: (p) => `/app/users/${p.uuid}/email`,
    params: {
      uuid: z.string().describe('User UUID'),
      email: z.string().describe('New email address'),
    },
  }),

  defineApiTool({
    name: 'set_user_roles',
    description: 'Set roles for a user (replaces existing roles).',
    group: 'users.write',
    method: 'PUT',
    path: (p) => `/app/users/${p.uuid}/roles`,
    params: {
      uuid: z.string().describe('User UUID'),
      roles: z.array(z.string()).describe('Role names to assign'),
    },
  }),

  defineApiTool({
    name: 'generate_access_key',
    description: 'Generate an API access key for a user.',
    group: 'users.write',
    method: 'POST',
    path: (p) => `/app/users/${p.uuid}/access-key`,
    params: {
      uuid: z.string().describe('User UUID'),
    },
  }),

  defineApiTool({
    name: 'revoke_access_key',
    description: 'Revoke a user API access key.',
    group: 'users.write',
    method: 'DELETE',
    path: (p) => `/app/users/${p.uuid}/access-key`,
    params: {
      uuid: z.string().describe('User UUID'),
    },
  }),

  defineApiTool({
    name: 'list_user_members',
    description: 'List members under a user account.',
    group: 'users',
    path: (p) => `/app/users/${p.uuid}/members`,
    params: {
      uuid: z.string().describe('User UUID'),
    },
  }),

  defineApiTool({
    name: 'add_user_member',
    description: 'Add a member to a user account by email.',
    group: 'users.write',
    method: 'POST',
    path: (p) => `/app/users/${p.uuid}/members`,
    params: {
      uuid: z.string().describe('User UUID'),
      member_email: z.string().describe('Email of the member to add'),
    },
    mapBody: (p) => ({ email: p.member_email }),
  }),

  defineApiTool({
    name: 'remove_user_member',
    description: 'Remove a member from a user account.',
    group: 'users.write',
    method: 'DELETE',
    path: (p) => `/app/users/${p.uuid}/members/${p.member_uuid}`,
    params: {
      uuid: z.string().describe('User UUID'),
      member_uuid: z.string().describe('UUID of the member to remove'),
    },
  }),

  defineApiTool({
    name: 'update_user_retailer_status',
    description: 'Update whether a user is flagged as a retailer.',
    group: 'users.write',
    method: 'PUT',
    path: (p) => `/app/users/${p.uuid}/retailer-status`,
    params: {
      uuid: z.string().describe('User UUID'),
      is_retailer: z.boolean().describe('Whether the user is a retailer'),
    },
    mapBody: (p) => ({ isRetailer: p.is_retailer }),
  }),

  defineApiTool({
    name: 'hard_delete_users',
    description: 'Permanently delete user accounts. This action is irreversible.',
    group: 'users.write',
    method: 'POST',
    path: '/app/users/hard-delete',
    params: {
      user_uuids: z.array(z.string()).describe('UUIDs of users to delete'),
    },
    mapBody: (p) => ({ userUuids: p.user_uuids }),
  }),
]
