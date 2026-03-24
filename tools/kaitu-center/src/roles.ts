/**
 * Role-based tool access control.
 *
 * Maps role names to lists of tool names that the role is allowed to use.
 * Used by both MCP entry (index.ts, via KAITU_ROLE env) and
 * OpenClaw entry (openclaw.ts, via pluginConfig.role).
 */

export const TOOL_ROLES: Record<string, string[]> = {
  devops: [
    'list_nodes', 'exec_on_node', 'ping_node', 'delete_node',
    'query_device_logs', 'download_device_log',
    'query_feedback_tickets', 'resolve_feedback_ticket',
  ],
  support: [
    'lookup_user', 'list_user_devices',
    'query_device_logs', 'download_device_log',
    'query_feedback_tickets', 'resolve_feedback_ticket',
    'close_feedback_ticket',
  ],
  marketing: [
    'lookup_user',
    'list_retailers', 'get_retailer_detail', 'update_retailer_level',
    'create_retailer_note', 'list_retailer_todos',
    'list_edm_templates', 'create_edm_task',
    'preview_edm_targets', 'get_edm_send_stats',
  ],
}

/**
 * Returns the list of allowed tool names for the given role.
 * Defaults to 'devops' for unknown or missing roles.
 */
export function getToolsForRole(role: string): string[] {
  return TOOL_ROLES[role] ?? TOOL_ROLES.devops
}
