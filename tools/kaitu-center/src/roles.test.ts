import { describe, it, expect } from 'vitest'
import { getToolsForRole, TOOL_ROLES } from './roles.js'

describe('TOOL_ROLES', () => {
  it('devops role includes node ops tools', () => {
    expect(TOOL_ROLES.devops).toContain('list_nodes')
    expect(TOOL_ROLES.devops).toContain('exec_on_node')
    expect(TOOL_ROLES.devops).toContain('ping_node')
    expect(TOOL_ROLES.devops).toContain('delete_node')
  })

  it('devops role includes shared log/ticket tools', () => {
    expect(TOOL_ROLES.devops).toContain('query_device_logs')
    expect(TOOL_ROLES.devops).toContain('download_device_log')
    expect(TOOL_ROLES.devops).toContain('query_feedback_tickets')
    expect(TOOL_ROLES.devops).toContain('resolve_feedback_ticket')
  })

  it('support role includes user lookup and ticket tools', () => {
    expect(TOOL_ROLES.support).toContain('lookup_user')
    expect(TOOL_ROLES.support).toContain('list_user_devices')
    expect(TOOL_ROLES.support).toContain('query_device_logs')
    expect(TOOL_ROLES.support).toContain('close_feedback_ticket')
  })

  it('support role does NOT include node ops', () => {
    expect(TOOL_ROLES.support).not.toContain('list_nodes')
    expect(TOOL_ROLES.support).not.toContain('exec_on_node')
  })

  it('marketing role includes retailer and EDM tools', () => {
    expect(TOOL_ROLES.marketing).toContain('list_retailers')
    expect(TOOL_ROLES.marketing).toContain('create_edm_task')
    expect(TOOL_ROLES.marketing).toContain('lookup_user')
  })

  it('marketing role does NOT include node ops or tickets', () => {
    expect(TOOL_ROLES.marketing).not.toContain('exec_on_node')
    expect(TOOL_ROLES.marketing).not.toContain('query_feedback_tickets')
  })
})

describe('getToolsForRole', () => {
  it('returns devops tools for unknown role', () => {
    expect(getToolsForRole('unknown')).toEqual(TOOL_ROLES.devops)
  })

  it('returns devops tools when role is undefined', () => {
    expect(getToolsForRole(undefined as unknown as string)).toEqual(TOOL_ROLES.devops)
  })

  it('returns correct tools for each known role', () => {
    expect(getToolsForRole('devops')).toEqual(TOOL_ROLES.devops)
    expect(getToolsForRole('support')).toEqual(TOOL_ROLES.support)
    expect(getToolsForRole('marketing')).toEqual(TOOL_ROLES.marketing)
  })
})
