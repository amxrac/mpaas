export type DeploymentStatus =
  | 'pending'
  | 'building'
  | 'deploying'
  | 'running'
  | 'failed'
  | 'stopped'

export interface CreateDeploymentInput {
  github_url: string
  port?: number
  root_dir?: string
  env?: Record<string, string>
}

export interface Deployment {
  id: string
  name: string
  status: DeploymentStatus
  github_url: string
  image_name: string
  container_name: string
  container_port: number
  root_dir?: string
  caddy_route: string
  created_at: string
  updated_at: string
}

export interface LogEntry {
  id: string
  deployment_id: string
	message:      string
	created_at:   string
	deployment:   Deployment
}
