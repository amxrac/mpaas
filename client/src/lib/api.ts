const API_URL = "/api";

export async function api(path: string, init?: RequestInit): Promise<Response> {
  const headers: Record<string, string> = {
    ...(init?.headers as Record<string, string>),
  };

  if (!(init?.body instanceof FormData)) {
    headers["Content-Type"] = "application/json";
  }

  const res = await fetch(`${API_URL}${path}`, {
    ...init,
    headers,
  });
  return res;
}

export async function createDeployment(github_url: string, port?: number) {
  const res: Response = await api("/deployments", {
    method: "POST",
    body: JSON.stringify({ github_url: github_url, port })
  });

  if (!res.ok) {
    throw new Error("Failed to create deployment");
  }

  return res.json()
}

export async function getDeployment(id: string) {
  const res: Response = await api(`/deployments/${id}`, {
    method: "GET",
  });

  if (!res.ok) {
    throw new Error("Failed to get deployment");
  }

  return res.json()
}

export async function listDeployments() {
  const res: Response = await api("/deployments", {
      method: "GET",
    });

    if (!res.ok) {
      throw new Error("Failed to get deployments");
    }

    return res.json()
}

export async function deleteDeployment(id: string) {
  const res = await api(`/deployments/${id}`, {
    method: 'DELETE'
  });

  if (!res.ok) {
    throw new Error('Failed to delete deployment');
  }

  return res.json();
}

export async function stopDeployment(id: string) {
  const res = await api(`/deployments/${id}/stop`, {
    method: 'POST'
  });

  if (!res.ok) {
    throw new Error('Failed to stop deployment');
  }

  return res.json();
}

export function streamDeploymentLogs(id: string) {
  return new EventSource(`/api/deployments/${id}/logs/stream`);
}
