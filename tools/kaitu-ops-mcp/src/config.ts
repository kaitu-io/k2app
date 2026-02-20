// Stub — will be replaced in GREEN phase
export interface CenterConfig {
  url: string
  accessKey: string
}

export interface SshConfig {
  privateKeyPath: string
  user: string
  port: number
}

export interface Config {
  center: CenterConfig
  ssh: SshConfig
}

export async function loadConfig(_configPath?: string): Promise<Config> {
  throw new Error('not implemented')
}
