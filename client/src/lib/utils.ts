import type { Deployment } from './types';

export function getDisplayName(dep: Deployment): string {
  const cleaned = dep.github_url.replace(/\/+$/, '').replace(/\.git$/, '');
  return cleaned.split('/').slice(-2).join('/');
}
