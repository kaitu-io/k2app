/**
 * Tests for the ssh.ts SSH helper module.
 *
 * Uses vi.mock('ssh2') to avoid requiring a real SSH server.
 * Each test controls the mock Client's event emission directly.
 */

import { describe, it, expect, vi, beforeEach, type MockInstance } from 'vitest'
import type { SshConfig } from './config.ts'
import * as fs from 'node:fs'

// ---------------------------------------------------------------------------
// Mock ssh2 before importing the module under test.
// We create a mock Client class whose instances we can control per-test.
// ---------------------------------------------------------------------------

// Shared mutable reference to the most-recently created mock client instance.
// Tests will assign to this to control behaviour.
let mockClientInstance: MockSsh2Client | null = null

interface MockSsh2Client {
  connect: MockInstance
  exec: MockInstance
  end: MockInstance
  on: MockInstance
  once: MockInstance
  // Internal helpers to simulate events in tests
  _triggerReady: () => void
  _triggerError: (err: Error) => void
}

// Build a fresh MockClient factory used by vi.mock below.
function createMockClient(): MockSsh2Client {
  const listeners: Record<string, ((...args: unknown[]) => void)[]> = {}

  const client: MockSsh2Client = {
    connect: vi.fn(),
    exec: vi.fn(),
    end: vi.fn(),
    on: vi.fn((event: string, cb: (...args: unknown[]) => void) => {
      if (!listeners[event]) listeners[event] = []
      listeners[event]!.push(cb)
      return client
    }),
    once: vi.fn((event: string, cb: (...args: unknown[]) => void) => {
      // For once, we store it the same way; triggering is manual in tests.
      if (!listeners[event]) listeners[event] = []
      listeners[event]!.push(cb)
      return client
    }),
    _triggerReady: () => {
      for (const cb of listeners['ready'] ?? []) cb()
    },
    _triggerError: (err: Error) => {
      for (const cb of listeners['error'] ?? []) cb(err)
    },
  }

  return client
}

vi.mock('ssh2', () => {
  return {
    Client: vi.fn(() => {
      const instance = createMockClient()
      mockClientInstance = instance
      return instance
    }),
  }
})

// ---------------------------------------------------------------------------
// Mock fs.readFileSync so tests never hit the real filesystem for private keys.
// ---------------------------------------------------------------------------
vi.mock('node:fs', async (importOriginal) => {
  const actual = await importOriginal<typeof fs>()
  return {
    ...actual,
    readFileSync: vi.fn((path: unknown, _enc?: unknown) => {
      if (typeof path === 'string' && path.includes('id_rsa')) {
        return '-----BEGIN RSA PRIVATE KEY-----\nfakekey\n-----END RSA PRIVATE KEY-----'
      }
      // Fall through to original for other paths (e.g. actual test fixtures)
      return actual.readFileSync(path as fs.PathOrFileDescriptor, _enc as BufferEncoding)
    }),
  }
})

// ---------------------------------------------------------------------------
// Import the module under test AFTER mocks are set up.
// ---------------------------------------------------------------------------
import { sshExec, sshExecWithStdin } from './ssh.js'

// ---------------------------------------------------------------------------
// Shared test SSH config
// ---------------------------------------------------------------------------
const TEST_SSH_CONFIG: SshConfig = {
  privateKeyPath: '/home/user/.ssh/id_rsa',
  user: 'root',
  port: 22,
}

// Helper: build a mock exec channel that emits stdout, stderr, and exit.
function makeMockChannel(opts: {
  stdout?: string
  stderr?: string
  exitCode?: number
  delayExitMs?: number
}): unknown {
  const stdoutListeners: ((chunk: Buffer) => void)[] = []
  const stderrListeners: ((chunk: Buffer) => void)[] = []
  const exitListeners: ((code: number) => void)[] = []
  const closeListeners: (() => void)[] = []

  const stderrStream = {
    on: vi.fn((event: string, cb: (chunk: Buffer) => void) => {
      if (event === 'data') stderrListeners.push(cb)
      return stderrStream
    }),
  }

  const channel = {
    stdout: null as unknown,
    stderr: stderrStream,
    on: vi.fn((event: string, cb: ((...args: unknown[]) => void)) => {
      if (event === 'data') stdoutListeners.push(cb as (chunk: Buffer) => void)
      if (event === 'exit') exitListeners.push(cb as (code: number) => void)
      if (event === 'close') closeListeners.push(cb as () => void)
      return channel
    }),
    write: vi.fn((_data: unknown, _encoding?: unknown, cb?: () => void) => {
      if (typeof cb === 'function') cb()
      return true
    }),
    end: vi.fn(),
    // Expose trigger helpers
    _emit: () => {
      const delay = opts.delayExitMs ?? 0
      setTimeout(() => {
        if (opts.stdout) {
          for (const cb of stdoutListeners) cb(Buffer.from(opts.stdout!))
        }
        if (opts.stderr) {
          for (const cb of stderrListeners) cb(Buffer.from(opts.stderr!))
        }
        for (const cb of exitListeners) cb(opts.exitCode ?? 0)
        for (const cb of closeListeners) cb()
      }, delay)
    },
  }

  return channel
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('sshExec', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockClientInstance = null
  })

  it('test_ssh_connect_with_key — calls ssh2 Client.connect with correct host/port/username/privateKey', async () => {
    const channel = makeMockChannel({ stdout: 'hello', exitCode: 0 }) as ReturnType<typeof makeMockChannel> & { _emit: () => void }

    // We need to intercept exec calls before triggerReady is called.
    // Strategy: set up exec mock after client is created via connect hook.
    const execPromise = sshExec('10.0.0.1', TEST_SSH_CONFIG, 'echo hello')

    // At this point mockClientInstance is set. Configure exec to use our channel.
    const client = mockClientInstance!
    client.exec.mockImplementation((_cmd: unknown, cb: (err: Error | null, ch: unknown) => void) => {
      cb(null, channel)
      ;(channel as { _emit: () => void })._emit()
    })

    // Trigger the ready event so the module proceeds to call exec.
    client._triggerReady()

    const result = await execPromise

    // Verify connect was called with correct parameters
    expect(client.connect).toHaveBeenCalledOnce()
    const connectArg = client.connect.mock.calls[0]![0] as Record<string, unknown>
    expect(connectArg['host']).toBe('10.0.0.1')
    expect(connectArg['port']).toBe(22)
    expect(connectArg['username']).toBe('root')
    expect(typeof connectArg['privateKey']).toBe('string')
    expect(result.stdout).toBe('hello')
    expect(result.exitCode).toBe(0)
  })

  it('test_ssh_connection_refused — connection error → "Connection refused to {host}:{port}"', async () => {
    const execPromise = sshExec('10.0.0.2', TEST_SSH_CONFIG, 'echo test')

    const client = mockClientInstance!
    const err = new Error('connect ECONNREFUSED 10.0.0.2:22') as Error & { code?: string }
    err.code = 'ECONNREFUSED'
    client._triggerError(err)

    await expect(execPromise).rejects.toThrow('Connection refused to 10.0.0.2:22')
  })

  it('test_ssh_auth_failed — auth error → "Authentication failed for {host}"', async () => {
    const execPromise = sshExec('10.0.0.3', TEST_SSH_CONFIG, 'echo test')

    const client = mockClientInstance!
    const err = new Error('All configured authentication methods failed') as Error & { level?: string }
    err.level = 'client-authentication'
    client._triggerError(err)

    await expect(execPromise).rejects.toThrow('Authentication failed for 10.0.0.3')
  })

  it('test_exec_basic — command returns {stdout, stderr, exitCode}', async () => {
    const channel = makeMockChannel({ stdout: 'command output', exitCode: 0 }) as ReturnType<typeof makeMockChannel> & { _emit: () => void }

    const execPromise = sshExec('10.0.0.4', TEST_SSH_CONFIG, 'echo hi')

    const client = mockClientInstance!
    client.exec.mockImplementation((_cmd: unknown, cb: (err: Error | null, ch: unknown) => void) => {
      cb(null, channel)
      ;(channel as { _emit: () => void })._emit()
    })
    client._triggerReady()

    const result = await execPromise

    expect(result.stdout).toBe('command output')
    expect(result.stderr).toBe('')
    expect(result.exitCode).toBe(0)
  })

  it('test_exec_stderr — stderr captured separately from stdout', async () => {
    const channel = makeMockChannel({
      stdout: 'out',
      stderr: 'error output',
      exitCode: 1,
    }) as ReturnType<typeof makeMockChannel> & { _emit: () => void }

    const execPromise = sshExec('10.0.0.5', TEST_SSH_CONFIG, 'cmd')

    const client = mockClientInstance!
    client.exec.mockImplementation((_cmd: unknown, cb: (err: Error | null, ch: unknown) => void) => {
      cb(null, channel)
      ;(channel as { _emit: () => void })._emit()
    })
    client._triggerReady()

    const result = await execPromise

    expect(result.stdout).toBe('out')
    expect(result.stderr).toBe('error output')
    expect(result.exitCode).toBe(1)
  })

  it('test_exec_timeout — timeout exceeded → exitCode=-1, error in stderr', async () => {
    // Channel that never fires exit — will be killed by timeout
    const channel = makeMockChannel({ stdout: '', exitCode: 0, delayExitMs: 9999 }) as ReturnType<typeof makeMockChannel> & { _emit: () => void }

    const execPromise = sshExec('10.0.0.6', TEST_SSH_CONFIG, 'sleep 100', 50)

    const client = mockClientInstance!
    client.exec.mockImplementation((_cmd: unknown, cb: (err: Error | null, ch: unknown) => void) => {
      cb(null, channel)
      // Don't emit — let timeout fire
    })
    client._triggerReady()

    const result = await execPromise

    expect(result.exitCode).toBe(-1)
    expect(result.stderr).toContain('timed out')
  }, 2000)
})

describe('sshExecWithStdin', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockClientInstance = null
  })

  it('pipes stdinData to the channel before closing', async () => {
    const channel = makeMockChannel({ stdout: 'script ran', exitCode: 0 }) as ReturnType<typeof makeMockChannel> & { _emit: () => void; write: MockInstance; end: MockInstance }

    const execPromise = sshExecWithStdin('10.0.0.7', TEST_SSH_CONFIG, 'bash -s', '#!/bin/bash\necho hi')

    const client = mockClientInstance!
    client.exec.mockImplementation((_cmd: unknown, cb: (err: Error | null, ch: unknown) => void) => {
      cb(null, channel)
      ;(channel as { _emit: () => void })._emit()
    })
    client._triggerReady()

    const result = await execPromise

    expect(result.stdout).toBe('script ran')
    expect(result.exitCode).toBe(0)
    // write should have been called with the stdin data
    expect((channel as { write: MockInstance }).write).toHaveBeenCalledWith(
      '#!/bin/bash\necho hi',
      expect.anything(),
      expect.any(Function),
    )
    expect((channel as { end: MockInstance }).end).toHaveBeenCalled()
  })
})
